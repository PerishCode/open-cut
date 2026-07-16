package mediatoolchain

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseBaselineRejectsAValidButReducedDevelopmentCatalog(t *testing.T) {
	verified := Verified{Capabilities: make(map[string]Capability)}
	for _, id := range releaseBaselineCapabilities {
		verified.Capabilities[id] = Capability{
			ID: id,
			Entry: Tool{
				ID: id, Path: filepath.Join(t.TempDir(), "entry"),
			},
			ConformanceProfile: "fixture-v1",
			ClosureSHA256:      "sha256:" + strings.Repeat("a", 64),
		}
	}
	if err := VerifyReleaseBaseline(verified); err != nil {
		t.Fatal(err)
	}
	delete(verified.Capabilities, CapabilitySequencePreviewRendererV1)
	if err := VerifyReleaseBaseline(verified); err == nil ||
		!strings.Contains(err.Error(), CapabilitySequencePreviewRendererV1) {
		t.Fatalf("reduced catalog error=%v", err)
	}
}
