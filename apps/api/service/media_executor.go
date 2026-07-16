package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

const (
	mediaIdentifyExecutorCommand = "identify-v1"
	maximumExecutorMessageBytes  = 64 << 10
	maximumExecutorOutputBytes   = 16 << 10
)

type ExternalMediaIdentifyExecutor struct {
	access     *SourceAccess
	executable string
	tempRoot   string
	profile    lifecycle.Profile
	wallTime   time.Duration
}

func (executor *ExternalMediaIdentifyExecutor) Registration() application.MediaExecutorRegistration {
	return application.MediaExecutorRegistration{Kind: domain.MediaJobIdentify, Version: application.InitialMediaProducer}
}

func (executor *ExternalMediaIdentifyExecutor) Execute(
	ctx context.Context,
	claim application.MediaJobClaim,
) (application.MediaJobExecution, error) {
	identified, err := executor.Identify(ctx, claim)
	if err != nil {
		return application.MediaJobExecution{}, err
	}
	return application.MediaJobExecution{Identification: &identified}, nil
}

func NewExternalMediaIdentifyExecutor(
	access *SourceAccess,
	executable string,
	tempRoot string,
	profile lifecycle.Profile,
) (*ExternalMediaIdentifyExecutor, error) {
	if access == nil || !cleanAbsolute(executable) || !cleanAbsolute(tempRoot) ||
		(profile != lifecycle.ProfileProduction && profile != lifecycle.ProfilePackaged &&
			profile != lifecycle.ProfileDevelopment && profile != lifecycle.ProfileHarness) {
		return nil, fmt.Errorf("media identify executor configuration is invalid")
	}
	if info, err := os.Stat(executable); err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("media identify executor is unavailable")
	}
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create media attempt root: %w", err)
	}
	return &ExternalMediaIdentifyExecutor{
		access: access, executable: executable, tempRoot: tempRoot, profile: profile, wallTime: 30 * time.Minute,
	}, nil
}

func (executor *ExternalMediaIdentifyExecutor) Identify(
	ctx context.Context,
	claim application.MediaJobClaim,
) (application.MediaIdentification, error) {
	if claim.Kind != domain.MediaJobIdentify || claim.AttemptID.IsZero() || claim.AssetID.IsZero() {
		return application.MediaIdentification{}, application.NewMediaExecutionError(
			"executor-input-invalid", application.ErrMediaSourceRead,
		)
	}
	source, err := executor.access.resolveAssetSource(ctx, claim.AssetID)
	if err != nil {
		return application.MediaIdentification{}, err
	}
	if source.Observation != claim.ExpectedObservation {
		return application.MediaIdentification{}, application.NewMediaSourceExecutionError(
			"source-observation-changed", domain.AssetChanged, application.ErrMediaSourceMoved,
		)
	}
	attemptRoot := filepath.Join(executor.tempRoot, claim.AttemptID.String())
	if !pathWithin(executor.tempRoot, attemptRoot) {
		return application.MediaIdentification{}, application.NewMediaExecutionError(
			"executor-input-invalid", application.ErrMediaSourceRead,
		)
	}
	if err := os.Mkdir(attemptRoot, 0o700); err != nil {
		return application.MediaIdentification{}, application.NewMediaExecutionError(
			"attempt-storage-unavailable", err,
		)
	}
	defer os.RemoveAll(attemptRoot)
	input, err := json.Marshal(mediaIdentifyInput{Path: source.Path, ExpectedObservation: source.Observation})
	if err != nil {
		return application.MediaIdentification{}, err
	}
	executionContext, cancel := context.WithTimeout(ctx, executor.wallTime)
	defer cancel()
	stdout := &boundedBuffer{limit: maximumExecutorOutputBytes}
	stderr := &boundedBuffer{limit: maximumExecutorOutputBytes}
	err = lifecycle.Run(executionContext, lifecycle.ProcessSpec{
		Executable: executor.executable, Args: []string{"__media-executor", mediaIdentifyExecutorCommand},
		Directory: attemptRoot, Env: executorEnvironment(), Stdin: bytes.NewReader(input),
		Stdout: stdout, Stderr: stderr, Profile: executor.profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 2 * time.Second,
	})
	if err != nil || stdout.exceeded || stderr.exceeded {
		if _, sourceErr := executor.access.resolveAssetSource(ctx, claim.AssetID); sourceErr != nil {
			return application.MediaIdentification{}, sourceErr
		}
		code := "executor-failed"
		if errors.Is(executionContext.Err(), context.DeadlineExceeded) {
			code = "executor-timeout"
		}
		return application.MediaIdentification{}, application.NewMediaExecutionError(
			code, errors.New("isolated identify executor did not complete"),
		)
	}
	var output application.MediaIdentification
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&output); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		output.Observation != source.Observation {
		return application.MediaIdentification{}, application.NewMediaExecutionError(
			"executor-output-invalid", errors.New("identify output failed validation"),
		)
	}
	if _, err := domain.ParseDigest(output.Fingerprint.String()); err != nil {
		return application.MediaIdentification{}, application.NewMediaExecutionError(
			"executor-output-invalid", err,
		)
	}
	return output, nil
}

type mediaIdentifyInput struct {
	Path                string                   `json:"path"`
	ExpectedObservation domain.SourceObservation `json:"expectedObservation"`
}

func RunMediaExecutor(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) != 1 || args[0] != mediaIdentifyExecutorCommand || stdin == nil || stdout == nil {
		return fmt.Errorf("unsupported media executor command")
	}
	limited := io.LimitReader(stdin, maximumExecutorMessageBytes+1)
	message, err := io.ReadAll(limited)
	if err != nil || len(message) == 0 || len(message) > maximumExecutorMessageBytes {
		return fmt.Errorf("media executor input is invalid")
	}
	var input mediaIdentifyInput
	decoder := json.NewDecoder(bytes.NewReader(message))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		!cleanAbsolute(input.Path) || len(input.Path) > 32768 || input.ExpectedObservation.FileIdentity == "" {
		return fmt.Errorf("media executor input is invalid")
	}
	file, err := os.Open(input.Path)
	if err != nil {
		return fmt.Errorf("media source is unreadable")
	}
	defer file.Close()
	beforeInfo, err := file.Stat()
	if err != nil || !beforeInfo.Mode().IsRegular() {
		return fmt.Errorf("media source is unreadable")
	}
	before, err := sourceObservation(file, beforeInfo)
	if err != nil || before != input.ExpectedObservation {
		return fmt.Errorf("media source observation changed")
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return fmt.Errorf("media source is unreadable")
	}
	afterInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("media source is unreadable")
	}
	after, err := sourceObservation(file, afterInfo)
	if err != nil || after != before {
		return fmt.Errorf("media source observation changed")
	}
	fingerprint, err := domain.ParseDigest("sha256:" + hex.EncodeToString(digest.Sum(nil)))
	if err != nil {
		return fmt.Errorf("media fingerprint is invalid")
	}
	return json.NewEncoder(stdout).Encode(application.MediaIdentification{
		Fingerprint: fingerprint, Observation: after,
	})
}

type boundedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (buffer *boundedBuffer) Write(value []byte) (int, error) {
	if buffer.exceeded {
		return len(value), nil
	}
	remaining := buffer.limit - buffer.Len()
	if len(value) > remaining {
		buffer.exceeded = true
		if remaining > 0 {
			_, _ = buffer.Buffer.Write(value[:remaining])
		}
		return len(value), nil
	}
	return buffer.Buffer.Write(value)
}

func executorEnvironment() []string {
	allowed := map[string]struct{}{
		"LANG": {}, "LC_ALL": {}, "SYSTEMROOT": {}, "WINDIR": {}, "TMPDIR": {}, "TEMP": {}, "TMP": {},
	}
	result := make([]string, 0, len(allowed))
	for _, entry := range os.Environ() {
		name, _, found := bytes.Cut([]byte(entry), []byte{'='})
		if _, ok := allowed[string(name)]; found && ok {
			result = append(result, entry)
		}
	}
	return result
}

func cleanAbsolute(value string) bool {
	return value != "" && filepath.IsAbs(value) && filepath.Clean(value) == value
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != "." && relative != ".." && !filepath.IsAbs(relative) &&
		len(relative) > 0 && relative[:1] != "."
}
