package timingreport

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecorderKeepsOrderedPhasesAndDecisions(t *testing.T) {
	now := time.Unix(100, 0)
	recorder := newRecorder("pack", map[string]string{"target": "mac-arm64", "empty": ""}, func() time.Time {
		current := now
		now = now.Add(125 * time.Millisecond)
		return current
	})
	finish := recorder.Begin("workspace-build")
	finish(nil)
	recorder.Decide("media-closure", "rebuilt", "renderer source changed")
	failure := recorder.Begin("archive")
	failure(errors.New("archive failed"))
	report := recorder.Finish(errors.New("pack failed"))

	if report.Outcome != OutcomeFailed || report.DurationMS != 625 || report.Attributes["target"] != "mac-arm64" {
		t.Fatalf("report=%+v", report)
	}
	if len(report.Phases) != 2 || report.Phases[0].DurationMS != 125 || report.Phases[1].Outcome != OutcomeFailed {
		t.Fatalf("phases=%+v", report.Phases)
	}
	if len(report.Decisions) != 1 || report.Decisions[0].Value != "rebuilt" {
		t.Fatalf("decisions=%+v", report.Decisions)
	}
	finish(errors.New("second finish must be ignored"))
	if len(recorder.phases) != 2 {
		t.Fatalf("phase finish was not idempotent: %+v", recorder.phases)
	}
}

func TestReportRoundTripAndMarkdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "timing.json")
	report := Report{
		Schema: Schema, Operation: "pack", Outcome: OutcomeSucceeded, DurationMS: 1234,
		Attributes: map[string]string{"target": "linux-x64"},
		Decisions:  []Decision{{Name: "media-closure", Value: "reused"}},
		Phases:     []Phase{{Name: "workspace-build", Outcome: OutcomeSucceeded, DurationMS: 900}},
	}
	if err := Write(path, report); err != nil {
		t.Fatal(err)
	}
	loaded, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Operation != report.Operation || loaded.DurationMS != report.DurationMS {
		t.Fatalf("loaded=%+v", loaded)
	}
	markdown := Markdown([]Report{loaded})
	for _, expected := range []string{"pack · linux-x64", "media-closure", "workspace-build", "1.234s"} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("markdown omitted %q:\n%s", expected, markdown)
		}
	}
}

func TestDecisionValueRequiresOneExactDecision(t *testing.T) {
	report := Report{Decisions: []Decision{{Name: "c-build-tree", Value: "rebuilt"}}}
	value, err := DecisionValue(report, "c-build-tree")
	if err != nil || value != "rebuilt" {
		t.Fatalf("value=%q error=%v", value, err)
	}
	if _, err := DecisionValue(report, "missing"); err == nil {
		t.Fatal("missing decision was accepted")
	}
	report.Decisions = append(report.Decisions, Decision{Name: "c-build-tree", Value: "reused"})
	if _, err := DecisionValue(report, "c-build-tree"); err == nil {
		t.Fatal("duplicate decision was accepted")
	}
}

func TestStepClosesTheActivePhaseAndFailureClosesTheLast(t *testing.T) {
	now := time.Unix(200, 0)
	recorder := newRecorder("build", nil, func() time.Time {
		current := now
		now = now.Add(50 * time.Millisecond)
		return current
	})
	recorder.Step("reuse-check")
	recorder.Step("cold-build")
	report := recorder.Finish(errors.New("build failed"))
	if len(report.Phases) != 2 || report.Phases[0].Outcome != OutcomeSucceeded ||
		report.Phases[1].Outcome != OutcomeFailed || report.Phases[1].Detail != "build failed" {
		t.Fatalf("report=%+v", report)
	}
}

func TestComparisonMarkdownMatchesPhasesAndReuseDecisions(t *testing.T) {
	baseline := Report{
		Schema: Schema, Operation: "pack", Outcome: OutcomeSucceeded, DurationMS: 2000,
		Attributes: map[string]string{"target": "mac-arm64"},
		Decisions:  []Decision{{Name: "launcher", Value: "built"}},
		Phases: []Phase{
			{Name: "workspace-build", Outcome: OutcomeSucceeded, DurationMS: 1500},
			{Name: "archive", Outcome: OutcomeSucceeded, DurationMS: 500},
		},
	}
	candidate := Report{
		Schema: Schema, Operation: "pack", Outcome: OutcomeSucceeded, DurationMS: 1500,
		Attributes: map[string]string{"target": "mac-arm64"},
		Decisions:  []Decision{{Name: "launcher", Value: "prebuilt"}},
		Phases: []Phase{
			{Name: "workspace-build", Outcome: OutcomeSucceeded, DurationMS: 1000},
			{Name: "stage", Outcome: OutcomeSucceeded, DurationMS: 200},
		},
	}
	markdown, err := ComparisonMarkdown(baseline, candidate)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Timing comparison · pack · mac-arm64", "`built`", "`prebuilt`", "-500ms", "-25.0%",
		"| archive | 500ms | n/a | n/a | n/a |", "| stage | n/a | 200ms | n/a | n/a |",
	} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("comparison omitted %q:\n%s", expected, markdown)
		}
	}
}

func TestComparisonRejectsDifferentTargets(t *testing.T) {
	baseline := Report{Schema: Schema, Operation: "pack", Attributes: map[string]string{"target": "mac-arm64"}}
	candidate := Report{Schema: Schema, Operation: "pack", Attributes: map[string]string{"target": "linux-x64"}}
	if _, err := ComparisonMarkdown(baseline, candidate); err == nil {
		t.Fatal("comparison accepted different targets")
	}
}
