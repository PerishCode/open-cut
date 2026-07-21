package whispertoolchain

import (
	"fmt"
	"slices"
	"strings"

	"github.com/PerishCode/open-cut/internal/toolchainclosure"
	"github.com/PerishCode/open-cut/utils/target"
)

const (
	// toolchainVersion is this closure's own identity line. It deliberately
	// names only whisper.cpp: the whole point of the split is that bumping it
	// must not disturb any other closure.
	toolchainVersion = "whisper-1.8.6-open-cut.1"

	SourceVersion = "1.8.6"
	SourceURL     = "https://github.com/ggerganov/whisper.cpp/archive/refs/tags/v1.8.6.tar.gz"
	SourceSHA256  = "sha256:f8e632016ceae556f3132a16c7f704be1e7715595041f474fa81a2b64c1abf7c"

	WhisperLicenseNoticeID = "whisper.cpp-license"

	ConformanceModelID      = "whisper-tiny-conformance-model-v1"
	ConformanceModelVersion = "whisper.cpp-1.8.6-test-tiny"
	ConformanceModelSource  = "models/for-tests-ggml-tiny.bin"
	ConformanceModelFile    = "ggml-tiny.bin"
	ConformanceResourceRoot = "whisper/resources/whisper-tiny-conformance-model-v1"

	// BackendCPU is the portable baseline every public target can build.
	BackendCPU = "cpu"
	// BackendMetal adds the Apple GPU backend and the Accelerate framework.
	// It changes floating-point association, so token confidences differ in
	// their low-order digits from a CPU build. Transcript text and timings are
	// unaffected, and the semantic-stability contract is per machine, so this
	// is a legitimate backend rather than a determinism violation.
	BackendMetal = "metal"
)

// Version is this closure's identity, independent of every other toolchain.
func Version() string { return toolchainVersion }

func sourceRecords() []SourceRecord {
	return []SourceRecord{{
		ID: "whisper.cpp", Version: SourceVersion, URL: SourceURL,
		SHA256: SourceSHA256, License: "MIT",
	}}
}

// Backend selects the acceleration backend for a target. Absence of an
// accelerated backend is a typed property of the target, not a failure: every
// public target still qualifies the capability on its portable CPU build.
func Backend(buildTarget target.Target) string {
	if buildTarget.Platform == target.Mac && buildTarget.Arch == target.ARM64 {
		return BackendMetal
	}
	return BackendCPU
}

func validBackend(backend string, buildTarget target.Target) bool {
	return backend == Backend(buildTarget)
}

// configuration is the exact CMake contract for a target and backend.
//
// GGML_NATIVE stays OFF on every backend: tuning the build to the machine that
// happens to run it would make the published artifact depend on the build
// host's CPU. Metal buys its speedup from the GPU, not from host-specific
// instruction selection, so the two are independent.
func configuration(
	buildTarget target.Target, backend, sourceRoot, compiler, cxx string,
) ([]string, error) {
	if buildTarget.Validate() != nil || sourceRoot == "" || compiler == "" || cxx == "" ||
		backend != Backend(buildTarget) {
		return nil, fmt.Errorf("whisper.cpp build contract is invalid")
	}
	result := []string{
		"-DCMAKE_BUILD_TYPE=Release",
		"-DBUILD_SHARED_LIBS=OFF",
		"-DCMAKE_SKIP_RPATH=ON",
		"-DCMAKE_C_COMPILER=" + compiler,
		"-DCMAKE_CXX_COMPILER=" + cxx,
		"-DCMAKE_C_FLAGS_RELEASE=-O2 -DNDEBUG -ffile-prefix-map=" + sourceRoot + "=.",
		"-DCMAKE_CXX_FLAGS_RELEASE=-O2 -DNDEBUG -ffile-prefix-map=" + sourceRoot + "=.",
		"-DWHISPER_BUILD_TESTS=OFF",
		"-DWHISPER_BUILD_SERVER=OFF",
		"-DWHISPER_BUILD_EXAMPLES=ON",
		"-DWHISPER_CURL=OFF",
		"-DWHISPER_SDL2=OFF",
		"-DWHISPER_COMMON_FFMPEG=OFF",
		"-DWHISPER_COREML=OFF",
		"-DWHISPER_OPENVINO=OFF",
		"-DGGML_NATIVE=OFF",
		"-DGGML_OPENMP=OFF",
		"-DGGML_BLAS=OFF",
		"-DGGML_CUDA=OFF",
		"-DGGML_VULKAN=OFF",
		"-DGGML_RPC=OFF",
		"-DGGML_BACKEND_DL=OFF",
		"-DGGML_CCACHE=OFF",
		"-DGGML_LTO=OFF",
	}
	switch backend {
	case BackendMetal:
		// The shader library is embedded so the closure stays self-contained:
		// no runtime lookup of a sibling .metallib, nothing outside the
		// verified byte set.
		result = append(result,
			"-DGGML_METAL=ON", "-DGGML_METAL_EMBED_LIBRARY=ON", "-DGGML_ACCELERATE=ON",
		)
	case BackendCPU:
		result = append(result, "-DGGML_METAL=OFF", "-DGGML_ACCELERATE=OFF")
	default:
		return nil, fmt.Errorf("whisper.cpp backend is unsupported")
	}
	if buildTarget.Platform == target.Win {
		result = append(result, "-DCMAKE_EXE_LINKER_FLAGS=-static")
	}
	switch buildTarget.Arch {
	case target.ARM64:
		result = append(result, "-DGGML_CPU_ARM_ARCH=armv8-a")
	case target.X64:
		result = append(result,
			"-DGGML_SSE42=OFF", "-DGGML_AVX=OFF", "-DGGML_F16C=OFF", "-DGGML_FMA=OFF",
			"-DGGML_AVX2=OFF", "-DGGML_BMI2=OFF", "-DGGML_AVX_VNNI=OFF",
			"-DGGML_AVX512=OFF", "-DGGML_AVX512_VBMI=OFF", "-DGGML_AVX512_VNNI=OFF",
			"-DGGML_AVX512_BF16=OFF", "-DGGML_AMX_TILE=OFF", "-DGGML_AMX_INT8=OFF",
			"-DGGML_AMX_BF16=OFF",
		)
	default:
		return nil, fmt.Errorf("whisper.cpp target architecture is unsupported")
	}
	return result, nil
}

func normalizedConfiguration(values []string, sourceRoot, compiler, cxx string) []string {
	result := make([]string, len(values))
	replacements := []struct{ actual, token string }{
		{sourceRoot, "$whisper"}, {compiler, "$cc"}, {cxx, "$cxx"},
	}
	for index, value := range values {
		for _, replacement := range replacements {
			value = strings.ReplaceAll(value, replacement.actual, replacement.token)
		}
		result[index] = value
	}
	return result
}

func validConfiguration(values []string, backend string, buildTarget target.Target) bool {
	expected, err := configuration(buildTarget, backend, "$whisper", "$cc", "$cxx")
	return err == nil && slices.Equal(values, expected)
}

// recipeDigest is this closure's build identity: the pinned source, the target,
// the backend and the normalized configuration. Nothing from another toolchain
// participates, so no other closure's change can move it.
func recipeDigest(buildTarget target.Target, backend string, values []string) (string, error) {
	return toolchainclosure.ClosureDigest("open-cut/whisper-recipe/v1", struct {
		Schema        int           `json:"schema"`
		Version       string        `json:"version"`
		Target        target.Target `json:"target"`
		Backend       string        `json:"backend"`
		Source        SourceRecord  `json:"source"`
		Configuration []string      `json:"configuration"`
	}{
		Schema: 1, Version: toolchainVersion, Target: buildTarget, Backend: backend,
		Source: sourceRecords()[0], Configuration: values,
	})
}
