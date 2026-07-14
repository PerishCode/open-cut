package controlcli

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
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
