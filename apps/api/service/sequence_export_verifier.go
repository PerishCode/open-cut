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
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type ExternalSequenceExportVerifier struct {
	probe    string
	tempRoot string
	profile  lifecycle.Profile
	wallTime time.Duration
}

func NewExternalSequenceExportVerifier(
	probe string,
	tempRoot string,
	profile lifecycle.Profile,
) (*ExternalSequenceExportVerifier, error) {
	if !cleanAbsolute(probe) || !cleanAbsolute(tempRoot) ||
		(profile != lifecycle.ProfileProduction && profile != lifecycle.ProfilePackaged &&
			profile != lifecycle.ProfileDevelopment && profile != lifecycle.ProfileHarness) {
		return nil, fmt.Errorf("sequence export verifier configuration is invalid")
	}
	if info, err := os.Stat(probe); err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("sequence export verifier probe is unavailable")
	}
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create sequence export verifier root: %w", err)
	}
	return &ExternalSequenceExportVerifier{
		probe: probe, tempRoot: tempRoot, profile: profile, wallTime: 12 * time.Hour,
	}, nil
}

func (verifier *ExternalSequenceExportVerifier) Verify(
	ctx context.Context,
	request application.SequenceExportVerificationRequest,
) (domain.RenderedMediaFacts, error) {
	if request.Claim.JobID.IsZero() || request.Claim.AttemptID.IsZero() ||
		request.Claim.Kind != domain.WorkJobSequenceExport || request.Claim.SequenceExport == nil ||
		request.Workspace == nil || request.Media.Path != "export.webm" ||
		request.Media.MimeType != "video/webm" || request.Media.ByteSize.Value() == 0 {
		return domain.RenderedMediaFacts{}, application.NewSequenceExportExecutionError(
			"renderer-output-invalid", application.ErrSequenceExportInvalid,
		)
	}
	expected, err := application.SequenceExportFactsForPlan(request.Plan.Plan.Payload)
	if err != nil {
		return domain.RenderedMediaFacts{}, application.NewSequenceExportExecutionError(
			"renderer-output-invalid", err,
		)
	}
	root := filepath.Join(verifier.tempRoot, request.Claim.AttemptID.String())
	if !pathWithin(verifier.tempRoot, root) {
		return domain.RenderedMediaFacts{}, application.ErrSequenceExportInvalid
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		return domain.RenderedMediaFacts{}, application.NewSequenceExportExecutionError(
			"verification-storage-unavailable", err,
		)
	}
	defer os.RemoveAll(root)
	executionContext, cancel := context.WithTimeout(ctx, verifier.wallTime)
	defer cancel()
	document, err := verifier.probeStructure(executionContext, root, request)
	if err != nil {
		return domain.RenderedMediaFacts{}, verifier.verificationFailure(ctx, executionContext, err)
	}
	if err := renderengine.ValidateRenderedMediaProbeDocument(document, expected); err != nil {
		return domain.RenderedMediaFacts{}, application.NewSequenceExportExecutionError(
			"renderer-output-invalid", err,
		)
	}
	samples, err := verifier.countAudioSamples(executionContext, root, request)
	if err != nil {
		return domain.RenderedMediaFacts{}, verifier.verificationFailure(ctx, executionContext, err)
	}
	if samples != expected.AudioSampleCount.Value() {
		return domain.RenderedMediaFacts{}, application.NewSequenceExportExecutionError(
			"renderer-output-invalid",
			fmt.Errorf("audio sample count is %d, expected %d", samples, expected.AudioSampleCount.Value()),
		)
	}
	return expected, nil
}

func (verifier *ExternalSequenceExportVerifier) probeStructure(
	ctx context.Context,
	directory string,
	request application.SequenceExportVerificationRequest,
) (renderengine.SequencePreviewProbeDocument, error) {
	source, err := request.Workspace.Open(request.Media.Path)
	if err != nil {
		return renderengine.SequencePreviewProbeDocument{}, err
	}
	defer source.Close()
	digest := sha256.New()
	counter := &countingReader{reader: io.TeeReader(
		io.LimitReader(source, int64(request.Media.ByteSize.Value())+1), digest,
	)}
	stdout := &boundedBuffer{limit: maximumProbeOutputBytes}
	stderr := &boundedBuffer{limit: maximumExecutorOutputBytes}
	err = lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: verifier.probe,
		Args: []string{
			"-v", "error", "-hide_banner", "-protocol_whitelist", "pipe",
			"-count_frames", "-show_entries",
			"format=format_name:stream=index,codec_name,codec_type,width,height,avg_frame_rate,pix_fmt," +
				"color_range,color_space,color_transfer,color_primaries,sample_rate,channels,channel_layout,nb_read_frames",
			"-of", "json=compact=1", "pipe:0",
		},
		Directory: directory, Env: executorEnvironment(), Stdin: counter, Stdout: stdout, Stderr: stderr,
		Profile: verifier.profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 5 * time.Second,
	})
	if err != nil || stdout.exceeded || stderr.exceeded {
		return renderengine.SequencePreviewProbeDocument{}, fmt.Errorf(
			"export structure probe failed: %s", strings.TrimSpace(stderr.String()),
		)
	}
	if counter.count != request.Media.ByteSize.Value() ||
		"sha256:"+hex.EncodeToString(digest.Sum(nil)) != request.Media.SHA256.String() {
		return renderengine.SequencePreviewProbeDocument{}, fmt.Errorf("export payload digest or size mismatch")
	}
	var document renderengine.SequencePreviewProbeDocument
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return renderengine.SequencePreviewProbeDocument{}, fmt.Errorf("export structure probe returned invalid JSON")
	}
	return document, nil
}

func (verifier *ExternalSequenceExportVerifier) countAudioSamples(
	ctx context.Context,
	directory string,
	request application.SequenceExportVerificationRequest,
) (uint64, error) {
	source, err := request.Workspace.Open(request.Media.Path)
	if err != nil {
		return 0, err
	}
	defer source.Close()
	collector := renderengine.NewAudioSampleCollector(maximumSequencePreviewSampleReportBytes)
	stderr := &boundedBuffer{limit: maximumExecutorOutputBytes}
	err = lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: verifier.probe,
		Args: []string{
			"-v", "error", "-hide_banner", "-protocol_whitelist", "pipe",
			"-select_streams", "a:0", "-show_frames", "-show_entries", "frame=nb_samples",
			"-of", "csv=p=0", "pipe:0",
		},
		Directory: directory, Env: executorEnvironment(), Stdin: source, Stdout: collector, Stderr: stderr,
		Profile: verifier.profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 5 * time.Second,
	})
	if err != nil || stderr.exceeded {
		return 0, fmt.Errorf("export audio sample probe failed: %s", strings.TrimSpace(stderr.String()))
	}
	return collector.Finish()
}

func (verifier *ExternalSequenceExportVerifier) verificationFailure(
	parent context.Context,
	execution context.Context,
	cause error,
) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	code := "renderer-output-invalid"
	if errors.Is(execution.Err(), context.DeadlineExceeded) {
		code = "renderer-verification-timeout"
	}
	return application.NewSequenceExportExecutionError(code, cause)
}

var _ application.SequenceExportArtifactVerifier = (*ExternalSequenceExportVerifier)(nil)
