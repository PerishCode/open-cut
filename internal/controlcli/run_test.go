package controlcli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReleaseDisplayVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"release", "display-version", "1.2.3-stable.4"}, &stdout, &stderr)
	if code != 0 || stdout.String() != "1.2.3\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestHarnessCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"harness", "broker", "--workspace", filepath.Join(t.TempDir(), "case")}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q output=%q", code, stderr.String(), stdout.String())
	}
}

func TestProtocolRejectsUnknownMode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"protocol", "unknown"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestBootstrapRejectsUnexpectedArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"bootstrap", "unexpected"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestWaitForControlExitWaitsForDescriptorRemoval(t *testing.T) {
	controlFile := filepath.Join(t.TempDir(), "control.json")
	if err := os.WriteFile(controlFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	removed := make(chan struct{})
	go func() {
		time.Sleep(25 * time.Millisecond)
		_ = os.Remove(controlFile)
		close(removed)
	}()
	if err := waitForControlExit(context.Background(), controlFile, time.Second); err != nil {
		t.Fatal(err)
	}
	<-removed
}
