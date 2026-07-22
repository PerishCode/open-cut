package whispertoolchain

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/internal/toolchainclosure"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestQualificationReceiptBindsExactWhisperClosure(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	verified := Verified{
		Root: root,
		Manifest: Manifest{
			Target: target.Host(), ToolchainID: ToolchainID, Version: toolchainVersion,
			Build: BuildRecord{RecipeSHA256: digest, Backend: Backend(target.Host())},
		},
		Capabilities: map[string]Capability{
			CapabilityLocalTranscriptionV1: {
				ID: CapabilityLocalTranscriptionV1, ClosureSHA256: digest,
				ConformanceSuiteSHA256: conformanceSuiteDigest(CapabilityLocalTranscriptionV1, target.Host()),
			},
		},
	}
	if err := writeQualificationReceipt(verified); err != nil {
		t.Fatal(err)
	}
	if err := VerifyQualificationReceipt(verified); err != nil {
		t.Fatal(err)
	}
	changed := verified
	changed.Manifest.Build.Backend = "changed"
	if err := VerifyQualificationReceipt(changed); err == nil {
		t.Fatal("whisper receipt accepted a different backend")
	}
}

func TestQualificationAndCacheIdentitiesAreTargetScoped(t *testing.T) {
	mac := target.Target{Platform: target.Mac, Arch: target.ARM64}
	linux := target.Target{Platform: target.Linux, Arch: target.X64}
	left, err := qualificationContractDigest(mac)
	if err != nil {
		t.Fatal(err)
	}
	right, err := qualificationContractDigest(linux)
	if err != nil {
		t.Fatal(err)
	}
	source, err := SourceCacheIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if !toolchainclosure.ValidDigest(left) || !toolchainclosure.ValidDigest(right) ||
		!toolchainclosure.ValidDigest(source) || left == right {
		t.Fatalf("qualification identities mac=%q linux=%q source=%q", left, right, source)
	}
}
