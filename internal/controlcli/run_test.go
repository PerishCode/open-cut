package controlcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/internal/harness"
	"github.com/PerishCode/open-cut/internal/timingreport"
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
	workspace := filepath.Join(t.TempDir(), "case")
	code := Run(context.Background(), []string{"harness", "broker", "--workspace", workspace}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q output=%q", code, stderr.String(), stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(workspace, "reports", "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report harness.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Checks) == 0 || report.Checks[0].DurationMS < 0 {
		t.Fatalf("report=%+v", report)
	}
	timing, err := timingreport.Read(filepath.Join(workspace, "reports", "timing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if timing.Operation != "harness:broker-registration-ready" || timing.Outcome != timingreport.OutcomeSucceeded ||
		len(timing.Phases) != len(report.Checks) {
		t.Fatalf("timing=%+v", timing)
	}
}

func TestProtocolRejectsUnknownMode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"protocol", "unknown"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestTimingSummaryRendersReports(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pack.json")
	if err := timingreport.Write(path, timingreport.Report{
		Schema: timingreport.Schema, Operation: "pack", Outcome: timingreport.OutcomeSucceeded,
		DurationMS: 42, Attributes: map[string]string{"target": "mac-arm64"},
		Phases: []timingreport.Phase{{Name: "workspace-build", Outcome: timingreport.OutcomeSucceeded, DurationMS: 40}},
	}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"timing", "summary", "--report", path}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "pack · mac-arm64") || !strings.Contains(stdout.String(), "workspace-build") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestTimingCompareRendersPhaseDelta(t *testing.T) {
	root := t.TempDir()
	baselinePath, candidatePath := filepath.Join(root, "baseline.json"), filepath.Join(root, "candidate.json")
	for path, duration := range map[string]int64{baselinePath: 100, candidatePath: 75} {
		if err := timingreport.Write(path, timingreport.Report{
			Schema: timingreport.Schema, Operation: "pack", Outcome: timingreport.OutcomeSucceeded,
			DurationMS: duration, Attributes: map[string]string{"target": "mac-arm64"},
			Phases: []timingreport.Phase{{Name: "workspace-build", Outcome: timingreport.OutcomeSucceeded, DurationMS: duration}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"timing", "compare", "--baseline", baselinePath, "--candidate", candidatePath,
	}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "Timing comparison · pack · mac-arm64") ||
		!strings.Contains(stdout.String(), "-25ms") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestTimingDecisionReadsOneExactValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "media.json")
	if err := timingreport.Write(path, timingreport.Report{
		Schema: timingreport.Schema, Operation: "media-toolchain-build", Outcome: timingreport.OutcomeSucceeded,
		Decisions: []timingreport.Decision{{Name: "c-build-tree", Value: "rebuilt"}},
		Phases:    []timingreport.Phase{},
	}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"timing", "decision", "--report", path, "--name", "c-build-tree",
	}, &stdout, &stderr)
	if code != 0 || stdout.String() != "rebuilt\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{
		"timing", "decision", "--report", path, "--name", "missing",
	}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "unavailable") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestTimingCacheReportClassifiesExactFallbackMissAndNotNeeded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"timing", "cache-report", "--output", path, "--target", "mac-arm64",
		"--attribute", "event=pull_request",
		"--cache", "source,source-a,source-a,true",
		"--cache", "c-build,cbuild-b,cbuild-a,false",
		"--cache", "closure,closure-b,,false",
		"--cache", "cold-inputs,,,not-needed",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	report, err := timingreport.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if report.Attributes["event"] != "pull_request" || len(report.Decisions) != 4 ||
		report.Decisions[0].Value != "exact" || report.Decisions[1].Value != "fallback" ||
		report.Decisions[2].Value != "miss" || report.Decisions[3].Value != "not-needed" {
		t.Fatalf("report=%+v", report)
	}
}

func TestDevRejectsUnexpectedArguments(t *testing.T) {
	for _, arguments := range [][]string{
		{"dev", "stop"},
		{"dev", "--repo", ".", "unexpected"},
		{"dev", "inspect", "--eval", "1", "unexpected"},
		{"dev", "record", "--output", "out.webm", "unexpected"},
	} {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), arguments, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", arguments, code, stdout.String(), stderr.String())
		}
	}
}

func TestDevHelpNeverStartsASession(t *testing.T) {
	// --help wins over argument validation by mature-CLI convention; the
	// load-bearing property is that no dev session starts.
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"dev", "stop", "--help"}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestUnknownCommandFailsClosed(t *testing.T) {
	for _, arguments := range [][]string{
		{"unknown-command"},
		{"release", "unknown"},
		{"harness", "unknown"},
		{"clean", "unexpected"},
	} {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), arguments, &stdout, &stderr)
		if code != 2 || stderr.Len() == 0 {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", arguments, code, stdout.String(), stderr.String())
		}
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
