package renderhelper

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestRunPublishesStrictFailureForInvalidExecution(t *testing.T) {
	root := physicalTempDir(t)
	executionPath := filepath.Join(root, renderengine.ExecutionFilename)
	if err := os.WriteFile(executionPath, []byte(`{"schema":4}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), executionPath); err == nil {
		t.Fatal("invalid execution unexpectedly succeeded")
	}
	encoded, err := os.ReadFile(filepath.Join(root, renderengine.ResultFilename))
	if err != nil {
		t.Fatal(err)
	}
	result, err := renderengine.DecodeResult(encoded)
	if err != nil || result.Status != renderengine.ResultFailed || result.Diagnostic == nil ||
		result.Diagnostic.Code != renderengine.ResultCodePlanInvalid ||
		result.Diagnostic.SubjectID != renderengine.ExecutionFilename {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestRunNeverOverwritesExistingResult(t *testing.T) {
	root := physicalTempDir(t)
	executionPath := filepath.Join(root, renderengine.ExecutionFilename)
	resultPath := filepath.Join(root, renderengine.ResultFilename)
	if err := os.WriteFile(executionPath, []byte(`{"schema":4}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resultPath, []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), executionPath); err == nil {
		t.Fatal("pre-existing result was accepted")
	}
	encoded, err := os.ReadFile(resultPath)
	if err != nil || string(encoded) != "owned" {
		t.Fatalf("existing result changed to %q: %v", encoded, err)
	}
}

func TestClassifyPreservesCaptionAndResourceIdentities(t *testing.T) {
	captionID, err := domain.ParseCaptionID("00000000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	classified := classify(
		fmtWrapped(renderengine.CaptionGlyphMissingError{CaptionID: captionID}),
		renderengine.ResultCodeInternal, "plan", "renderer",
	)
	var failure renderFailure
	if !errors.As(classified, &failure) || failure.diagnostic.Code != renderengine.ResultCodeGlyphMissing ||
		failure.diagnostic.SubjectID != captionID.String() {
		t.Fatalf("caption failure=%+v err=%v", failure, classified)
	}
	classified = classify(
		fmtWrapped(renderengine.ResourceLimitError{Subject: "caption-raster-bytes"}),
		renderengine.ResultCodeInternal, "plan", "renderer",
	)
	if !errors.As(classified, &failure) || failure.diagnostic.Code != renderengine.ResultCodeResourceLimit ||
		failure.diagnostic.SubjectID != "caption-raster-bytes" {
		t.Fatalf("resource failure=%+v err=%v", failure, classified)
	}
}

func fmtWrapped(err error) error { return fmt.Errorf("wrapped: %w", err) }

func physicalTempDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}
