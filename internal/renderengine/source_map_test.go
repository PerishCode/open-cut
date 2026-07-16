package renderengine

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestSourceMapSelectsExactFloorAndFirstFallback(t *testing.T) {
	encoded, err := application.EncodeSourceProxyTimeMap(
		[]int64{-2, 0, 3, 10}, []int64{100, 200, 300, 400},
	)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "video-time-map.bin")
	if err := os.WriteFile(filename, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	filename, err = filepath.EvalSymlinks(filename)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	source, err := OpenSourceMap(filename, domain.Digest("sha256:"+hex.EncodeToString(digest[:])))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if source.Count() != 4 {
		t.Fatalf("count=%d", source.Count())
	}
	cursor, err := source.NewCursor()
	if err != nil {
		t.Fatal(err)
	}
	timeBase := mustSourceMapTime(t, 1, 2)
	for _, fixture := range []struct {
		value, scale int64
		ordinal      uint64
	}{
		{-2, 1, 0}, {-1, 1, 0}, {0, 1, 1}, {149, 100, 1}, {3, 2, 2}, {100, 1, 3},
		{0, 1, 1},
	} {
		target := mustSourceMapTime(t, fixture.value, fixture.scale)
		selected, err := cursor.SelectFloor(target, timeBase)
		if err != nil || selected.Ordinal != fixture.ordinal {
			t.Fatalf("target=%+v selected=%+v err=%v", target, selected, err)
		}
	}

	corruptDigest := domain.Digest("sha256:" + strings.Repeat("0", 64))
	if _, err := OpenSourceMap(filename, corruptDigest); err == nil {
		t.Fatal("corrupt source map digest was accepted")
	}
}

func TestDecodeTraversalRequiresExplicitMonotonicRuns(t *testing.T) {
	tracker, err := NewDecodeTraversalTracker(10)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.BeginRun(); err != nil {
		t.Fatal(err)
	}
	for _, ordinal := range []uint64{3, 3, 7} {
		if err := tracker.Observe(ordinal); err != nil {
			t.Fatal(err)
		}
	}
	if tracker.Traversed() != 8 {
		t.Fatalf("traversed=%d", tracker.Traversed())
	}
	if err := tracker.Observe(2); err == nil {
		t.Fatal("backward ordinal was accepted inside one decode run")
	}
	if err := tracker.BeginRun(); err != nil {
		t.Fatal(err)
	}
	var limit ResourceLimitError
	if err := tracker.Observe(2); !errors.As(err, &limit) || limit.Subject != "decoded-video-frames" {
		t.Fatalf("error=%v", err)
	}
}

func TestVideoInstructionSourceTimeUsesHalfOpenOutputGrid(t *testing.T) {
	instruction := domain.RenderVideoInstruction{
		SourceRange: domain.TimeRange{
			Start: mustSourceMapTime(t, -1, 2), Duration: mustSourceMapTime(t, 1, 1),
		},
		TimelineRange: domain.TimeRange{
			Start: mustSourceMapTime(t, 1, 1), Duration: mustSourceMapTime(t, 1, 1),
		},
	}
	frameRate := mustSourceMapTime(t, 2, 1)
	if _, active, err := VideoInstructionSourceTime(instruction, 1, frameRate); err != nil || active {
		t.Fatalf("pre-range active=%v err=%v", active, err)
	}
	selected, active, err := VideoInstructionSourceTime(instruction, 2, frameRate)
	if err != nil || !active || selected.Value.Value() != -1 || selected.Scale != 2 {
		t.Fatalf("selected=%+v active=%v err=%v", selected, active, err)
	}
	selected, active, err = VideoInstructionSourceTime(instruction, 3, frameRate)
	if err != nil || !active || selected.Value.Value() != 0 || selected.Scale != 1 {
		t.Fatalf("selected=%+v active=%v err=%v", selected, active, err)
	}
	if _, active, err := VideoInstructionSourceTime(instruction, 4, frameRate); err != nil || active {
		t.Fatalf("half-open end active=%v err=%v", active, err)
	}

	instruction.TimelineRange.Start = mustSourceMapTime(t, -1, 1)
	if _, _, err := VideoInstructionSourceTime(instruction, 0, frameRate); err == nil {
		t.Fatal("negative timeline start was accepted")
	}
	instruction.TimelineRange.Start = mustSourceMapTime(t, 1, 1)
	instruction.TimelineRange.Duration = mustSourceMapTime(t, 2, 1)
	if _, _, err := VideoInstructionSourceTime(instruction, 2, frameRate); err == nil {
		t.Fatal("unequal source and timeline duration was accepted")
	}
}

func mustSourceMapTime(t *testing.T, value, scale int64) domain.RationalTime {
	t.Helper()
	if scale > int64(^uint32(0)>>1) {
		t.Fatal("fixture scale is out of range")
	}
	result, err := domain.NewRationalTime(value, int32(scale))
	if err != nil {
		t.Fatal(err)
	}
	return result
}
