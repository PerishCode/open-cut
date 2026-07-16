package mediatoolchain

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/utils/environment"
	"github.com/PerishCode/open-cut/utils/target"
)

const (
	WhisperSourceVersion = "1.8.6"
	WhisperSourceURL     = "https://github.com/ggerganov/whisper.cpp/archive/refs/tags/v1.8.6.tar.gz"
	WhisperSourceSHA256  = "sha256:f8e632016ceae556f3132a16c7f704be1e7715595041f474fa81a2b64c1abf7c"

	WhisperConformanceModelID      = "whisper-tiny-conformance-model-v1"
	WhisperConformanceModelVersion = "whisper.cpp-1.8.6-test-tiny"
	whisperConformanceModelSource  = "models/for-tests-ggml-tiny.bin"
	whisperConformanceModelFile    = "ggml-tiny.bin"
	whisperConformanceResourceRoot = "media/resources/whisper-tiny-conformance-model-v1"
)

func whisperSourceRecord() SourceRecord {
	return SourceRecord{
		ID: "whisper.cpp", Version: WhisperSourceVersion, URL: WhisperSourceURL,
		SHA256: WhisperSourceSHA256, License: "MIT",
	}
}

func extractWhisperSource(archive, destination string) (string, error) {
	root, err := extractSource(
		archive, destination, "whisper.cpp-"+WhisperSourceVersion, "CMakeLists.txt",
	)
	if err != nil {
		return "", fmt.Errorf("extract pinned whisper.cpp: %w", err)
	}
	return root, nil
}

func whisperConfiguration(buildTarget target.Target, sourceRoot, compiler, cxx string) ([]string, error) {
	if buildTarget.Validate() != nil || sourceRoot == "" || compiler == "" || cxx == "" {
		return nil, fmt.Errorf("whisper.cpp build contract is invalid")
	}
	configuration := []string{
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
		"-DGGML_METAL=OFF",
		"-DGGML_ACCELERATE=OFF",
		"-DGGML_BLAS=OFF",
		"-DGGML_CUDA=OFF",
		"-DGGML_VULKAN=OFF",
		"-DGGML_RPC=OFF",
		"-DGGML_BACKEND_DL=OFF",
		"-DGGML_CCACHE=OFF",
		"-DGGML_LTO=OFF",
	}
	if buildTarget.Platform == target.Win {
		configuration = append(configuration, "-DCMAKE_EXE_LINKER_FLAGS=-static")
	}
	switch buildTarget.Arch {
	case target.ARM64:
		configuration = append(configuration, "-DGGML_CPU_ARM_ARCH=armv8-a")
	case target.X64:
		configuration = append(configuration,
			"-DGGML_SSE42=OFF", "-DGGML_AVX=OFF", "-DGGML_F16C=OFF", "-DGGML_FMA=OFF",
			"-DGGML_AVX2=OFF", "-DGGML_BMI2=OFF", "-DGGML_AVX_VNNI=OFF",
			"-DGGML_AVX512=OFF", "-DGGML_AVX512_VBMI=OFF", "-DGGML_AVX512_VNNI=OFF",
			"-DGGML_AVX512_BF16=OFF", "-DGGML_AMX_TILE=OFF", "-DGGML_AMX_INT8=OFF",
			"-DGGML_AMX_BF16=OFF",
		)
	default:
		return nil, fmt.Errorf("whisper.cpp target architecture is unsupported")
	}
	return configuration, nil
}

func normalizedWhisperConfiguration(
	configuration []string,
	sourceRoot, compiler, cxx string,
) []string {
	result := make([]string, len(configuration))
	replacements := []struct{ actual, token string }{
		{sourceRoot, "$whisper"}, {compiler, "$cc"}, {cxx, "$cxx"},
	}
	for index, value := range configuration {
		for _, replacement := range replacements {
			value = strings.ReplaceAll(value, replacement.actual, replacement.token)
		}
		result[index] = value
	}
	return result
}

func validWhisperConfiguration(configuration []string, buildTarget target.Target) bool {
	expected, err := whisperConfiguration(buildTarget, "$whisper", "$cc", "$cxx")
	return err == nil && slices.Equal(configuration, expected)
}

func buildWhisperCLI(
	ctx context.Context,
	sourceRoot, buildRoot, cmake, compiler, cxx string,
	parallelism int,
	buildTarget target.Target,
	stdout, stderr io.Writer,
) (string, []string, error) {
	configuration, err := whisperConfiguration(buildTarget, sourceRoot, compiler, cxx)
	if err != nil {
		return "", nil, err
	}
	whisperBuildRoot := filepath.Join(buildRoot, "whisper")
	if err := os.MkdirAll(whisperBuildRoot, 0o700); err != nil {
		return "", nil, err
	}
	buildEnvironment := environment.Merge(os.Environ(), nil, map[string]string{
		"SOURCE_DATE_EPOCH": "0",
	})
	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: cmake,
		Args:       append([]string{"-S", sourceRoot, "-B", whisperBuildRoot}, configuration...),
		Directory:  sourceRoot, Env: buildEnvironment, Stdout: stdout, Stderr: stderr,
		Profile: lifecycle.ProfileDevelopment, Presentation: lifecycle.PresentationHeadless,
	}); err != nil {
		return "", nil, fmt.Errorf("configure pinned whisper.cpp: %w", err)
	}
	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: cmake,
		Args: []string{
			"--build", whisperBuildRoot, "--config", "Release", "--target", "whisper-cli",
			"--parallel", fmt.Sprint(parallelism),
		},
		Directory: sourceRoot, Env: buildEnvironment, Stdout: stdout, Stderr: stderr,
		Profile: lifecycle.ProfileDevelopment, Presentation: lifecycle.PresentationHeadless,
	}); err != nil {
		return "", nil, fmt.Errorf("build pinned whisper.cpp: %w", err)
	}
	executableName := buildTarget.ExecutableName("whisper-cli")
	candidates := []string{
		filepath.Join(whisperBuildRoot, "bin", executableName),
		filepath.Join(whisperBuildRoot, "bin", "Release", executableName),
	}
	var built string
	for _, candidate := range candidates {
		info, statErr := os.Lstat(candidate)
		if statErr == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 && info.Size() > 0 {
			if built != "" {
				return "", nil, fmt.Errorf("whisper.cpp build produced ambiguous CLI outputs")
			}
			built = candidate
		}
	}
	if built == "" {
		return "", nil, fmt.Errorf("whisper.cpp build did not produce whisper-cli")
	}
	return built, normalizedWhisperConfiguration(configuration, sourceRoot, compiler, cxx), nil
}

func stageWhisperConformanceModel(sourceRoot, stageRoot string) (ResourceRecord, error) {
	relative := filepath.ToSlash(filepath.Join(whisperConformanceResourceRoot, whisperConformanceModelFile))
	destination := filepath.Join(stageRoot, filepath.FromSlash(relative))
	if err := copyRegularFile(
		filepath.Join(sourceRoot, filepath.FromSlash(whisperConformanceModelSource)), destination, 0o600,
	); err != nil {
		return ResourceRecord{}, fmt.Errorf("stage whisper.cpp conformance model: %w", err)
	}
	digest, size, err := digestFile(destination)
	if err != nil {
		return ResourceRecord{}, err
	}
	record := ResourceRecord{
		ID: WhisperConformanceModelID, Kind: ResourceKindTranscriptionConformanceModel,
		Version: WhisperConformanceModelVersion, Root: whisperConformanceResourceRoot,
		Files: []ResourceFileRecord{{Path: whisperConformanceModelFile, SHA256: digest, ByteSize: size}},
	}
	record.SHA256, err = resourceClosureDigest(record)
	if err != nil {
		return ResourceRecord{}, err
	}
	return record, nil
}

func stageWhisperNotice(sourceRoot, stageRoot string) (NoticeRecord, error) {
	relative := "licenses/media/WHISPER.CPP-LICENSE"
	destination := filepath.Join(stageRoot, filepath.FromSlash(relative))
	if err := copyRegularFile(filepath.Join(sourceRoot, "LICENSE"), destination, 0o600); err != nil {
		return NoticeRecord{}, err
	}
	digest, size, err := digestFile(destination)
	if err != nil {
		return NoticeRecord{}, err
	}
	return NoticeRecord{ID: "whisper.cpp-license", Path: relative, SHA256: digest, ByteSize: size}, nil
}
