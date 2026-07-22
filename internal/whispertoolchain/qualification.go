package whispertoolchain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/toolchainclosure"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/utils/target"
)

const (
	QualificationReceiptName  = "whisper.qualification.json"
	qualificationProfile      = "whisper-capability-v1"
	qualificationDomain       = "open-cut/whisper-qualification/v1"
	qualificationSmokeTimeout = 30 * time.Second
)

type qualificationInput struct {
	Schema         int           `json:"schema"`
	Target         target.Target `json:"target"`
	ToolchainID    string        `json:"toolchainId"`
	Version        string        `json:"version"`
	RecipeSHA256   string        `json:"recipeSha256"`
	Backend        string        `json:"backend"`
	ReleaseProfile string        `json:"releaseProfile"`
	CapabilityID   string        `json:"capabilityId"`
	ClosureSHA256  string        `json:"closureSha256"`
	SuiteSHA256    string        `json:"suiteSha256"`
}

// EnsureQualification accepts only a receipt for this exact engine, model,
// evidence, target/backend, and suite. A miss replays the semantic stability
// qualification before atomically replacing the receipt.
func EnsureQualification(ctx context.Context, verified Verified) (bool, error) {
	if err := VerifyReleaseBaseline(verified); err != nil {
		return false, err
	}
	if err := VerifyQualificationReceipt(verified); err == nil {
		return true, nil
	}
	if err := VerifyCapabilities(ctx, verified); err != nil {
		return false, err
	}
	if err := writeQualificationReceipt(verified); err != nil {
		return false, err
	}
	return false, nil
}

func VerifyQualificationReceipt(verified Verified) error {
	input, err := qualificationIdentity(verified)
	if err != nil {
		return err
	}
	return toolchainclosure.VerifyQualificationReceipt(
		verified.Root, QualificationReceiptName, qualificationProfile, qualificationDomain, input,
	)
}

func writeQualificationReceipt(verified Verified) error {
	input, err := qualificationIdentity(verified)
	if err != nil {
		return err
	}
	return toolchainclosure.WriteQualificationReceipt(
		verified.Root, QualificationReceiptName, qualificationProfile, qualificationDomain, input,
	)
}

func qualificationIdentity(verified Verified) (qualificationInput, error) {
	capability, exists := verified.Capabilities[CapabilityLocalTranscriptionV1]
	if !exists || verified.Root == "" {
		return qualificationInput{}, fmt.Errorf("whisper qualification input is unavailable")
	}
	manifest := verified.Manifest
	return qualificationInput{
		Schema: 1, Target: manifest.Target, ToolchainID: manifest.ToolchainID,
		Version: manifest.Version, RecipeSHA256: manifest.Build.RecipeSHA256,
		Backend: manifest.Build.Backend, ReleaseProfile: ReleaseBaselineProfile,
		CapabilityID: capability.ID, ClosureSHA256: capability.ClosureSHA256,
		SuiteSHA256: capability.ConformanceSuiteSHA256,
	}, nil
}

// VerifySmoke proves that the exact deployed executable can start and still
// exposes the model/audio command surface. Semantic stability remains covered
// by the content-addressed qualification receipt.
func VerifySmoke(ctx context.Context, verified Verified) error {
	if err := VerifyReleaseBaseline(verified); err != nil {
		return err
	}
	capability := verified.Capabilities[CapabilityLocalTranscriptionV1]
	executionContext, cancel := context.WithTimeout(ctx, qualificationSmokeTimeout)
	defer cancel()
	stdout := &limitedBuffer{limit: 64 << 10}
	stderr := &limitedBuffer{limit: 64 << 10}
	if err := lifecycle.Run(executionContext, lifecycle.ProcessSpec{
		Executable: capability.Entry.Path, Args: []string{"--help"}, Directory: verified.Root,
		Env: conformanceEnvironment(), Stdout: stdout, Stderr: stderr,
		Profile: lifecycle.ProfilePackaged, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 5 * time.Second,
	}); err != nil {
		return fmt.Errorf("run deployed whisper smoke: %w", err)
	}
	if stdout.exceeded || stderr.exceeded {
		return fmt.Errorf("deployed whisper smoke output exceeds its bound")
	}
	output := strings.ToLower(stdout.String() + "\n" + stderr.String())
	if !strings.Contains(output, "usage:") || !strings.Contains(output, "--model") {
		return fmt.Errorf("deployed whisper smoke output is invalid")
	}
	return nil
}

// SourceCacheIdentity binds the shared source-archive cache to whisper's pin.
func SourceCacheIdentity() (string, error) {
	return toolchainclosure.ClosureDigest("open-cut/whisper-source-cache/v1", sourceRecords())
}

// ClosureCacheIdentity binds the co-located API artifact cache to whisper's
// build logic and qualification contract without coupling media build identity
// to whisper's recipe.
func ClosureCacheIdentity(repositoryRoot string, buildTarget target.Target) (string, error) {
	build, err := buildFingerprint(repositoryRoot)
	if err != nil {
		return "", err
	}
	contract, err := qualificationContractDigest(buildTarget)
	if err != nil {
		return "", err
	}
	return toolchainclosure.ClosureDigest("open-cut/whisper-closure-cache/v1", struct {
		Version  string        `json:"version"`
		Target   target.Target `json:"target"`
		Backend  string        `json:"backend"`
		Build    string        `json:"build"`
		Contract string        `json:"contract"`
	}{toolchainVersion, buildTarget, Backend(buildTarget), build, contract})
}

func qualificationContractDigest(buildTarget target.Target) (string, error) {
	return toolchainclosure.ClosureDigest(qualificationDomain+"/contract", struct {
		Schema         int           `json:"schema"`
		Target         target.Target `json:"target"`
		Backend        string        `json:"backend"`
		Profile        string        `json:"profile"`
		ReleaseProfile string        `json:"releaseProfile"`
		SuiteSHA256    string        `json:"suiteSha256"`
	}{
		Schema: 1, Target: buildTarget, Backend: Backend(buildTarget), Profile: qualificationProfile,
		ReleaseProfile: ReleaseBaselineProfile,
		SuiteSHA256:    conformanceSuiteDigest(CapabilityLocalTranscriptionV1, buildTarget),
	})
}
