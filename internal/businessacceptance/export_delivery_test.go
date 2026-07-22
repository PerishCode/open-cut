package businessacceptance

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyDeliveredExport(t *testing.T) {
	content := []byte("verified installed export")
	path := filepath.Join(t.TempDir(), "story.webm")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	observation := Observation{
		ExportByteSize:      fmt.Sprint(len(content)),
		ExportContentDigest: fmt.Sprintf("sha256:%x", sha256.Sum256(content)),
	}
	if err := verifyDeliveredExport(path, observation); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyDeliveredExport(path, observation); err == nil || err.Error() != "installed export destination has 7 bytes; expected 25" {
		t.Fatalf("changed installed export error = %v", err)
	}
	missing := filepath.Join(t.TempDir(), "missing.webm")
	if err := verifyDeliveredExport(missing, observation); err == nil || err.Error() != "installed export destination is missing" {
		t.Fatalf("missing installed export error = %v", err)
	}
}

func TestScenarioRejectsPartialNativeDeliveryOptions(t *testing.T) {
	_, err := RunCreatorToCLI(context.Background(), CreatorToCLIOptions{
		CDPEndpoint: "http://127.0.0.1:1", ProjectName: "story", FixturePath: "/fixture.wav",
		ExpectedAudioChannels: "2", RunIntent: "test", AuthoredText: "test", CLI: &scriptedCLI{},
		DeliveryPath: "/story.webm",
	})
	if err == nil || err.Error() != "Creator-to-CLI delivery options are incomplete" {
		t.Fatalf("partial delivery options error = %v", err)
	}
}
