package whispertoolchain

import (
	"context"
	"os"
	"testing"
)

// TestPinnedWhisperProducesClosedTranscriptionEvidence exercises the real
// qualification against a real binary and model. It needs no FFmpeg: the
// fixture is synthesized, which is the whole reason this closure can ship
// without one.
func TestPinnedWhisperProducesClosedTranscriptionEvidence(t *testing.T) {
	whisper := os.Getenv("OPEN_CUT_TRANSCRIPTION_CONFORMANCE_WHISPER")
	model := os.Getenv("OPEN_CUT_TRANSCRIPTION_CONFORMANCE_MODEL")
	if whisper == "" || model == "" {
		t.Skip("set transcription conformance whisper and model paths")
	}
	observations, err := Qualify(context.Background(), whisper, model)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 3 {
		t.Fatalf("local transcription observations=%d", len(observations))
	}
}

func TestConformanceFixtureIsCanonicalPCM(t *testing.T) {
	fixture := conformanceWAV()
	if len(fixture) != 44+16_000*2 {
		t.Fatalf("fixture bytes=%d", len(fixture))
	}
	for _, expected := range []struct {
		offset int
		value  string
	}{{0, "RIFF"}, {8, "WAVE"}, {12, "fmt "}, {36, "data"}} {
		if got := string(fixture[expected.offset : expected.offset+4]); got != expected.value {
			t.Fatalf("offset %d = %q, want %q", expected.offset, got, expected.value)
		}
	}
	// 16 kHz, mono, 16-bit is the exact shape the API normalizes to before it
	// ever invokes whisper.
	if rate := uint32(fixture[24]) | uint32(fixture[25])<<8; rate != 16_000 {
		t.Fatalf("sample rate=%d", rate)
	}
	if channels := uint16(fixture[22]) | uint16(fixture[23])<<8; channels != 1 {
		t.Fatalf("channels=%d", channels)
	}
	if bits := uint16(fixture[34]) | uint16(fixture[35])<<8; bits != 16 {
		t.Fatalf("bits=%d", bits)
	}
}
