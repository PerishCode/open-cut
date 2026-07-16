package transcriptadapter

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestWAVAndWhisperAdapterPreserveExactSourceTime(t *testing.T) {
	root := t.TempDir()
	wavPath := filepath.Join(root, "normalized.wav")
	pcm := make([]byte, 32_000*2)
	for index := range pcm {
		pcm[index] = byte(index % 251)
	}
	if err := os.WriteFile(wavPath, canonicalWAVFixture(pcm), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceStart, _ := domain.NewRationalTime(1, 2)
	proof, err := InspectWAV(wavPath, sourceStart, "stereo-equal-v1")
	if err != nil || proof.SampleCount.Value() != 32_000 || proof.PCMByteSize.Value() != 64_000 ||
		!strings.HasPrefix(proof.PCMDigest.String(), "sha256:") {
		t.Fatalf("proof=%+v err=%v", proof, err)
	}
	resultPath := filepath.Join(root, "recognition.json")
	if err := os.WriteFile(resultPath, []byte(whisperJSONFixture(false)), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := DecodeWhisper(resultPath, proof)
	if err != nil || result.DetectedLanguage != "en" || len(result.Segments) != 1 ||
		result.Segments[0].Text != "hello world" || len(result.Segments[0].Tokens) != 2 ||
		result.Segments[0].Tokens[0].Text != "hello" || result.Segments[0].Tokens[1].Text != " world" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.Segments[0].SourceRange.Start != sourceStart {
		t.Fatalf("source start=%+v", result.Segments[0].SourceRange.Start)
	}
	end, err := result.Segments[0].SourceRange.End()
	wantEnd, _ := domain.NewRationalTime(5, 2)
	if err != nil || end != wantEnd {
		t.Fatalf("end=%+v err=%v", end, err)
	}
}

func TestWhisperAdapterRejectsOpenOrFabricatedResult(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "recognition.json")
	proof := transcriptProofFixture()
	for _, data := range []string{
		whisperJSONFixture(true),
		strings.Replace(whisperJSONFixture(false), `"text":" hello world"`, `"text":" fabricated"`, 1),
		strings.Replace(whisperJSONFixture(false), `"to":2000`, `"to":0`, 1),
	} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if result, err := DecodeWhisper(path, proof); err == nil {
			t.Fatalf("invalid result accepted: %+v", result)
		}
	}
}

func TestWhisperAdapterCanonicalizesControlAndPointTokens(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "recognition.json")
	document := `{
  "systeminfo":"fixture-cpu",
  "model":{"type":"small","multilingual":true,"vocab":51865,
    "audio":{"ctx":1500,"state":768,"head":12,"layer":12},
    "text":{"ctx":448,"state":768,"head":12,"layer":12},"mels":80,"ftype":1},
  "params":{"model":"model.bin","language":"auto","translate":false},
  "result":{"language":"en"},
  "transcription":[{"timestamps":{"from":"00:00:00.000","to":"00:00:02.000"},
    "offsets":{"from":0,"to":2000},"text":" hello world.","tokens":[
      {"text":"[_BEG_]","timestamps":{"from":"00:00:00,000","to":"00:00:00,000"},
       "offsets":{"from":0,"to":0},"id":50364,"p":0.99,"t_dtw":-1},
      {"text":" hello","timestamps":{"from":"00:00:00.000","to":"00:00:01.000"},
       "offsets":{"from":0,"to":1000},"id":1,"p":0.98,"t_dtw":-1},
      {"text":" world","timestamps":{"from":"00:00:01.000","to":"00:00:01.900"},
       "offsets":{"from":1000,"to":1900},"id":2,"p":0.97,"t_dtw":-1},
      {"text":".","timestamps":{"from":"00:00:01,950","to":"00:00:01,950"},
       "offsets":{"from":1950,"to":1950},"id":13,"p":0.86,"t_dtw":-1},
      {"text":"[_TT_100]","timestamps":{"from":"00:00:02,000","to":"00:00:02,000"},
       "offsets":{"from":2000,"to":2000},"id":50464,"p":0.11,"t_dtw":-1}
    ]}]}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := DecodeWhisper(path, transcriptProofFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Segments) != 1 || result.Segments[0].Text != "hello world." ||
		len(result.Segments[0].Tokens) != 2 || result.Segments[0].Tokens[0].Text != "hello" ||
		result.Segments[0].Tokens[1].Text != " world." ||
		result.Segments[0].Tokens[1].ConfidenceBasisPoints != nil {
		t.Fatalf("canonical result=%+v", result)
	}
}

func canonicalWAVFixture(pcm []byte) []byte {
	result := bytes.NewBuffer(make([]byte, 0, 44+len(pcm)))
	result.WriteString("RIFF")
	_ = binary.Write(result, binary.LittleEndian, uint32(36+len(pcm)))
	result.WriteString("WAVEfmt ")
	_ = binary.Write(result, binary.LittleEndian, uint32(16))
	_ = binary.Write(result, binary.LittleEndian, uint16(1))
	_ = binary.Write(result, binary.LittleEndian, uint16(1))
	_ = binary.Write(result, binary.LittleEndian, uint32(16_000))
	_ = binary.Write(result, binary.LittleEndian, uint32(32_000))
	_ = binary.Write(result, binary.LittleEndian, uint16(2))
	_ = binary.Write(result, binary.LittleEndian, uint16(16))
	result.WriteString("data")
	_ = binary.Write(result, binary.LittleEndian, uint32(len(pcm)))
	result.Write(pcm)
	return result.Bytes()
}

func transcriptProofFixture() domain.TranscriptNormalizationProof {
	start, _ := domain.NewRationalTime(0, 1)
	samples, _ := domain.NewUInt64(32_000)
	bytes, _ := domain.NewUInt64(64_000)
	return domain.TranscriptNormalizationProof{
		SourceStartTime: start, SampleRate: 16_000, Channels: 1, SampleFormat: "s16le",
		SampleCount: samples, PCMByteSize: bytes,
		PCMDigest:     domain.Digest("sha256:" + strings.Repeat("a", 64)),
		ChannelPolicy: "mono-pass-v1", TimingPolicy: "audio-frame-pts-gap-fill-v1",
	}
}

func whisperJSONFixture(unknown bool) string {
	extra := ""
	if unknown {
		extra = `,"unexpected":true`
	}
	return `{
  "systeminfo":"fixture-cpu",
  "model":{"type":"small","multilingual":true,"vocab":51865,
    "audio":{"ctx":1500,"state":768,"head":12,"layer":12},
    "text":{"ctx":448,"state":768,"head":12,"layer":12},"mels":80,"ftype":1},
  "params":{"model":"model.bin","language":"auto","translate":false},
  "result":{"language":"en"},
  "transcription":[{"timestamps":{"from":"00:00:00.000","to":"00:00:02.000"},
    "offsets":{"from":0,"to":2000},"text":" hello world","tokens":[
      {"text":" hello","timestamps":{"from":"00:00:00.000","to":"00:00:01.000"},
       "offsets":{"from":0,"to":1000},"id":1,"p":0.98,"t_dtw":-1},
      {"text":" world","timestamps":{"from":"00:00:01.000","to":"00:00:02.000"},
       "offsets":{"from":1000,"to":2000},"id":2,"p":0.97,"t_dtw":-1}
    ]}]
` + extra + `}`
}
