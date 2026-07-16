package renderengine

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestBoundedProcessStreamUsesBackpressureAndExactLimit(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "stream.bin")
	spec := boundedStreamHelperSpec(t, root, output)
	value := strings.Repeat("bounded-stream-", 4_096)
	written, err := RunBoundedProcessStream(context.Background(), spec, uint64(len(value)), func(
		_ context.Context,
		destination io.Writer,
	) error {
		_, err := io.WriteString(destination, value)
		return err
	})
	if err != nil || written != uint64(len(value)) {
		t.Fatalf("written=%d err=%v", written, err)
	}
	stored, err := os.ReadFile(output)
	if err != nil || string(stored) != value {
		t.Fatalf("stored bytes=%d err=%v", len(stored), err)
	}
}

func TestBoundedProcessStreamRejectsBeforePartialChunk(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "stream.bin")
	spec := boundedStreamHelperSpec(t, root, output)
	written, err := RunBoundedProcessStream(context.Background(), spec, 3, func(
		_ context.Context,
		destination io.Writer,
	) error {
		_, err := destination.Write([]byte("four"))
		return err
	})
	var limit ResourceLimitError
	if !errors.As(err, &limit) || limit.Subject != "raw-stream-bytes" || written != 0 {
		t.Fatalf("written=%d limit=%+v err=%v", written, limit, err)
	}
	stored, readErr := os.ReadFile(output)
	if readErr != nil || len(stored) != 0 {
		t.Fatalf("stored=%q err=%v", stored, readErr)
	}
}

func TestBoundedProcessStreamPreservesStableCaptionFailure(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "stream.bin")
	spec := boundedStreamHelperSpec(t, root, output)
	captionID, err := domain.ParseCaptionID("00000000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	_, err = RunBoundedProcessStream(context.Background(), spec, 16, func(
		_ context.Context,
		_ io.Writer,
	) error {
		return CaptionGlyphMissingError{CaptionID: captionID}
	})
	var missing CaptionGlyphMissingError
	if !errors.As(err, &missing) || missing.CaptionID != captionID {
		t.Fatalf("stable caption failure was erased: %v", err)
	}
}

func boundedStreamHelperSpec(t *testing.T, root, output string) lifecycle.ProcessSpec {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle.ProcessSpec{
		Executable: executable,
		Args:       []string{"-test.run=TestBoundedProcessStreamHelper", "--"},
		Directory:  root,
		Env: append(os.Environ(),
			"OPEN_CUT_BOUNDED_STREAM_HELPER=1",
			"OPEN_CUT_BOUNDED_STREAM_OUTPUT="+output,
		),
		Stdout: io.Discard, Stderr: io.Discard,
		Profile: lifecycle.ProfileHarness, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: time.Second,
	}
}

func TestBoundedProcessStreamHelper(t *testing.T) {
	if os.Getenv("OPEN_CUT_BOUNDED_STREAM_HELPER") != "1" {
		return
	}
	value, err := io.ReadAll(os.Stdin)
	if err == nil {
		err = os.WriteFile(os.Getenv("OPEN_CUT_BOUNDED_STREAM_OUTPUT"), value, 0o600)
	}
	if err != nil {
		os.Exit(9)
	}
	os.Exit(0)
}
