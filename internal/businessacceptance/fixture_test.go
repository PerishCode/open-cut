package businessacceptance

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAudioFixtureIsDeterministicPCM(t *testing.T) {
	first := filepath.Join(t.TempDir(), "first.wav")
	second := filepath.Join(t.TempDir(), "second.wav")
	if err := WriteAudioFixture(first); err != nil {
		t.Fatal(err)
	}
	if err := WriteAudioFixture(second); err != nil {
		t.Fatal(err)
	}
	one, _ := os.ReadFile(first)
	two, _ := os.ReadFile(second)
	if string(one) != string(two) || string(one[:4]) != "RIFF" || string(one[8:12]) != "WAVE" {
		t.Fatal("generated WAV fixture is not deterministic PCM")
	}
	if got := binary.LittleEndian.Uint32(one[24:28]); got != fixtureSampleRate {
		t.Fatalf("sample rate=%d", got)
	}
}

func TestWriteSpeechFixtureRestoresPinnedWebM(t *testing.T) {
	first := filepath.Join(t.TempDir(), "first.webm")
	second := filepath.Join(t.TempDir(), "second.webm")
	if err := WriteSpeechFixture(first); err != nil {
		t.Fatal(err)
	}
	if err := WriteSpeechFixture(second); err != nil {
		t.Fatal(err)
	}
	one, _ := os.ReadFile(first)
	two, _ := os.ReadFile(second)
	if string(one) != string(two) || len(one) != 14_125 ||
		fmt.Sprintf("%x", sha256.Sum256(one)) != acceptanceSpeechFixtureSHA256 {
		t.Fatal("restored speech fixture is not the pinned WebM")
	}
}
