package mediatoolchain

import (
	"slices"
	"testing"

	"github.com/PerishCode/open-cut/utils/target"
)

func TestWhisperConfigurationPinsPortablePublicTargetCPU(t *testing.T) {
	for _, fixture := range []struct {
		target   target.Target
		required []string
	}{
		{
			target: target.Target{Platform: target.Mac, Arch: target.ARM64},
			required: []string{
				"-DGGML_NATIVE=OFF", "-DGGML_METAL=OFF", "-DGGML_ACCELERATE=OFF",
				"-DGGML_CPU_ARM_ARCH=armv8-a",
			},
		},
		{
			target: target.Target{Platform: target.Linux, Arch: target.X64},
			required: []string{
				"-DGGML_NATIVE=OFF", "-DGGML_SSE42=OFF", "-DGGML_AVX=OFF",
				"-DGGML_AVX2=OFF", "-DGGML_FMA=OFF", "-DGGML_F16C=OFF",
			},
		},
		{
			target: target.Target{Platform: target.Win, Arch: target.X64},
			required: []string{
				"-DGGML_NATIVE=OFF", "-DGGML_AVX=OFF", "-DCMAKE_EXE_LINKER_FLAGS=-static",
			},
		},
	} {
		configuration, err := whisperConfiguration(fixture.target, "$whisper", "$cc", "$cxx")
		if err != nil || !validWhisperConfiguration(configuration, fixture.target) {
			t.Fatalf("target=%s configuration=%q err=%v", fixture.target, configuration, err)
		}
		for _, required := range fixture.required {
			if !slices.Contains(configuration, required) {
				t.Fatalf("target=%s missing=%s", fixture.target, required)
			}
		}
	}
}

func TestLocalTranscriptionCapabilityHasExactClosedShape(t *testing.T) {
	resources := map[string]ResourceRecord{
		WhisperConformanceModelID: {
			ID: WhisperConformanceModelID, Kind: ResourceKindTranscriptionConformanceModel,
		},
	}
	record := CapabilityRecord{
		ID: CapabilityLocalTranscriptionV1, EntryToolID: "whisper-cli",
		ToolIDs:     []string{"ffmpeg", "ffprobe", "whisper-cli"},
		ResourceIDs: []string{WhisperConformanceModelID},
		NoticeIDs:   []string{"conformance-local-transcription-v1", "whisper.cpp-license"},
	}
	if err := validateCapabilityShape(record, resources); err != nil {
		t.Fatal(err)
	}
	record.ToolIDs = append(record.ToolIDs, "ambient-whisper")
	if err := validateCapabilityShape(record, resources); err == nil {
		t.Fatal("local transcription capability accepted an ambient tool")
	}
}
