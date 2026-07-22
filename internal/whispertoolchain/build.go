package whispertoolchain

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/PerishCode/open-cut/internal/timingreport"
	"github.com/PerishCode/open-cut/internal/toolchainclosure"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/environment"
	"github.com/PerishCode/open-cut/utils/target"
)

// buildCacheRoot is this closure's own source and build tree.
//
// It is deliberately not the media toolchain's tree. Sharing one meant a
// whisper change invalidated the FFmpeg/libvpx/opus build and every media CI
// cache layer, and the reverse. Separate roots are what make the two closures
// independently cacheable.
const buildCacheRoot = "whisper-toolchain"

type BuildOptions struct {
	RepositoryRoot string
	Target         target.Target
	Stdout         io.Writer
	Stderr         io.Writer
	TimingReport   string
}

type BuildResult struct {
	Schema        int    `json:"schema"`
	Target        string `json:"target"`
	Version       string `json:"version"`
	Backend       string `json:"backend"`
	Manifest      string `json:"manifest"`
	Transcriber   string `json:"transcriber"`
	TranscriberSH string `json:"transcriberSha256"`
	Reused        bool   `json:"reused"`
}

func Build(ctx context.Context, options BuildOptions) (result BuildResult, resultErr error) {
	recorder := timingreport.New("whisper-toolchain-build", map[string]string{
		"target": options.Target.String(), "version": toolchainVersion,
	})
	defer func() {
		report := recorder.Finish(resultErr)
		if options.TimingReport != "" {
			if err := timingreport.Write(options.TimingReport, report); resultErr == nil && err != nil {
				resultErr = fmt.Errorf("write whisper toolchain timing report: %w", err)
			}
		}
	}()
	repositoryRoot, err := filepath.Abs(options.RepositoryRoot)
	if err != nil {
		return BuildResult{}, err
	}
	repositoryRoot, err = filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return BuildResult{}, fmt.Errorf("whisper toolchain build requires a repository root")
	}
	if options.Target.Validate() != nil || options.Target != target.Host() {
		return BuildResult{}, fmt.Errorf("whisper toolchain source build requires the host public target")
	}
	stdout, stderr := options.Stdout, options.Stderr
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	backend := Backend(options.Target)
	artifactRoot := filepath.Join(repositoryRoot, "apps", "api", "dist", "sidecar")

	// A published closure that still matches the logic which produced it is
	// reused rather than rebuilt. Skipping this was a measurable regression:
	// the engine costs seconds to build on macOS but well over a minute under
	// MinGW, and it was being paid on every run including ones where every
	// other artifact came from cache.
	//
	// Load already proves the bytes — manifest identity, exact digests, the
	// recorded configuration for this target and backend, and every closure
	// digest — so the only thing it cannot see is a change to the build logic
	// that does not alter the manifest, such as a different qualification
	// suite. The recorded fingerprint covers exactly that.
	recorder.Step("inspect-published-closure")
	reused, reusable, reuseReason := inspectReuse(repositoryRoot, artifactRoot, options.Target)
	closureDecision := "rebuilt"
	if reusable {
		closureDecision = "reused"
	}
	recorder.Decide("published-closure", closureDecision, reuseReason)
	if reusable {
		capability := reused.Capabilities[CapabilityLocalTranscriptionV1]
		return BuildResult{
			Schema: ManifestSchema, Target: options.Target.String(), Version: reused.Manifest.Version,
			Backend:     reused.Manifest.Build.Backend,
			Manifest:    filepath.Join(artifactRoot, ManifestName),
			Transcriber: capability.Entry.Path, TranscriberSH: capability.Entry.SHA256,
			Reused: true,
		}, nil
	}
	fmt.Fprintf(stderr, "whisper toolchain cold build: %s\n", reuseReason)
	workRoot := filepath.Join(
		repositoryRoot, ".tmp", "oc-control", buildCacheRoot, options.Target.String(),
	)
	recorder.Step("prepare-build-workspace")
	sourceRoot := filepath.Join(workRoot, "source")
	buildRoot := filepath.Join(workRoot, "build")
	stageRoot := filepath.Join(workRoot, "stage")
	for _, directory := range []string{sourceRoot, buildRoot} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return BuildResult{}, err
		}
	}
	if err := os.RemoveAll(stageRoot); err != nil {
		return BuildResult{}, err
	}
	if err := os.MkdirAll(stageRoot, 0o700); err != nil {
		return BuildResult{}, err
	}

	recorder.Step("inspect-build-tools")
	cmake, err := exec.LookPath("cmake")
	if err != nil {
		return BuildResult{}, fmt.Errorf("whisper toolchain build requires cmake: %w", err)
	}
	compiler, cxx, err := resolveCompilers()
	if err != nil {
		return BuildResult{}, err
	}
	compilerIdentity, err := inspectBuildTools(ctx, compiler, cxx, cmake)
	if err != nil {
		return BuildResult{}, err
	}

	recorder.Step("ensure-pinned-source")
	source := sourceRecords()[0]
	archive, err := toolchainclosure.SourceArchivePath(sourceRoot, source)
	if err != nil {
		return BuildResult{}, err
	}
	if err := toolchainclosure.EnsureSource(ctx, archive, source); err != nil {
		return BuildResult{}, err
	}
	// The build tree is rebuilt from the pinned archive every time. A cold
	// whisper build costs seconds, not the many minutes a codec toolchain
	// costs, so there is nothing here worth the reuse machinery — and a tree
	// that is always freshly extracted cannot drift from its pin.
	recorder.Step("extract-pinned-source")
	if err := os.RemoveAll(buildRoot); err != nil {
		return BuildResult{}, err
	}
	if err := os.MkdirAll(buildRoot, 0o700); err != nil {
		return BuildResult{}, err
	}
	extracted, err := toolchainclosure.ExtractSource(
		archive, buildRoot, "whisper.cpp-"+SourceVersion, "CMakeLists.txt",
	)
	if err != nil {
		return BuildResult{}, fmt.Errorf("extract pinned whisper.cpp: %w", err)
	}

	recorder.Step("build-whisper-cli")
	builtWhisper, recordedConfiguration, err := buildWhisperCLI(
		ctx, extracted, buildRoot, cmake, compiler, cxx, backend,
		runtime.NumCPU(), options.Target, stdout, stderr,
	)
	if err != nil {
		return BuildResult{}, err
	}

	recorder.Step("assemble-staged-closure")
	whisperRelative := filepath.ToSlash(
		filepath.Join("whisper", options.Target.ExecutableName("whisper-cli")),
	)
	whisperPath := filepath.Join(stageRoot, filepath.FromSlash(whisperRelative))
	if err := copyRegularFile(builtWhisper, whisperPath, 0o755); err != nil {
		return BuildResult{}, err
	}
	whisperDigest, whisperSize, err := toolchainclosure.DigestFile(whisperPath)
	if err != nil {
		return BuildResult{}, err
	}
	toolRecords := []ToolRecord{{
		ID: ToolWhisperCLI, Path: whisperRelative, SHA256: whisperDigest, ByteSize: whisperSize,
	}}

	model, err := stageConformanceModel(extracted, stageRoot)
	if err != nil {
		return BuildResult{}, err
	}
	whisperNotice, err := stageWhisperNotice(extracted, stageRoot)
	if err != nil {
		return BuildResult{}, err
	}
	sourceNotice, err := stageSourceNotice(stageRoot, options.Target, backend, compilerIdentity, recordedConfiguration)
	if err != nil {
		return BuildResult{}, err
	}

	capability := capabilityRecord([]NoticeRecord{sourceNotice}, whisperNotice, model, options.Target)

	// Qualification runs the real binary against the real fixture. It proves
	// semantic stability on this machine and that a non-model is rejected; it
	// deliberately does not claim anything about other machines.
	recorder.Step("qualify-transcription-capability")
	observations, err := Qualify(ctx, whisperPath, filepath.Join(
		stageRoot, filepath.FromSlash(ConformanceResourceRoot), ConformanceModelFile,
	))
	if err != nil {
		return BuildResult{}, fmt.Errorf("qualify local transcription: %w", err)
	}
	toolIndex := map[string]ToolRecord{ToolWhisperCLI: toolRecords[0]}
	resourceIndex := map[string]ResourceRecord{model.ID: model}
	evidence, err := buildConformanceEvidence(
		options.Target, capability, toolIndex, resourceIndex, observations,
	)
	if err != nil {
		return BuildResult{}, err
	}
	evidenceNotice, err := writeConformanceEvidence(stageRoot, evidence)
	if err != nil {
		return BuildResult{}, err
	}

	recorder.Step("write-closure-manifest")
	recipe, err := recipeDigest(options.Target, backend, recordedConfiguration)
	if err != nil {
		return BuildResult{}, err
	}
	manifest := Manifest{
		Schema: ManifestSchema, Target: options.Target, ToolchainID: ToolchainID,
		Version: toolchainVersion, LicenseProfile: LicenseProfile,
		Sources: sourceRecords(),
		Build: BuildRecord{
			RecipeSHA256: recipe, Compiler: compilerIdentity, Backend: backend,
			Configuration: append([]string(nil), recordedConfiguration...),
		},
		Tools:        toolRecords,
		Resources:    []ResourceRecord{model},
		Capabilities: []CapabilityRecord{capability},
		Notices:      sortedNotices(sourceNotice, whisperNotice, evidenceNotice),
	}
	if err := bindManifestClosureDigests(&manifest); err != nil {
		return BuildResult{}, err
	}
	if err := validateManifest(manifest, options.Target); err != nil {
		return BuildResult{}, fmt.Errorf("whisper toolchain build produced an invalid manifest: %w", err)
	}
	manifestPath := filepath.Join(stageRoot, ManifestName)
	if err := atomicfile.WriteJSON(manifestPath, manifest, 0o600); err != nil {
		return BuildResult{}, err
	}
	recorder.Step("publish-closure")
	if err := publishStage(stageRoot, artifactRoot); err != nil {
		return BuildResult{}, err
	}
	recorder.Step("record-build-fingerprint")
	if err := writeBuildFingerprint(repositoryRoot, artifactRoot); err != nil {
		return BuildResult{}, err
	}
	return BuildResult{
		Schema: ManifestSchema, Target: options.Target.String(), Version: toolchainVersion,
		Backend: backend, Manifest: filepath.Join(artifactRoot, ManifestName),
		Transcriber:   filepath.Join(artifactRoot, filepath.FromSlash(whisperRelative)),
		TranscriberSH: whisperDigest,
	}, nil
}

func buildWhisperCLI(
	ctx context.Context,
	sourceRoot, buildRoot, cmake, compiler, cxx, backend string,
	parallelism int,
	buildTarget target.Target,
	stdout, stderr io.Writer,
) (string, []string, error) {
	values, err := configuration(buildTarget, backend, sourceRoot, compiler, cxx)
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
		Args:       append([]string{"-S", sourceRoot, "-B", whisperBuildRoot}, values...),
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
	return built, normalizedConfiguration(values, sourceRoot, compiler, cxx), nil
}

func stageConformanceModel(sourceRoot, stageRoot string) (ResourceRecord, error) {
	relative := filepath.ToSlash(filepath.Join(ConformanceResourceRoot, ConformanceModelFile))
	destination := filepath.Join(stageRoot, filepath.FromSlash(relative))
	if err := copyRegularFile(
		filepath.Join(sourceRoot, filepath.FromSlash(ConformanceModelSource)), destination, 0o600,
	); err != nil {
		return ResourceRecord{}, fmt.Errorf("stage whisper.cpp conformance model: %w", err)
	}
	digest, size, err := toolchainclosure.DigestFile(destination)
	if err != nil {
		return ResourceRecord{}, err
	}
	record := ResourceRecord{
		ID: ConformanceModelID, Kind: ResourceKindConformanceModel,
		Version: ConformanceModelVersion, Root: ConformanceResourceRoot,
		Files: []ResourceFileRecord{{Path: ConformanceModelFile, SHA256: digest, ByteSize: size}},
	}
	record.SHA256, err = toolchainclosure.ResourceClosureDigest(record)
	if err != nil {
		return ResourceRecord{}, err
	}
	return record, nil
}

func stageWhisperNotice(sourceRoot, stageRoot string) (NoticeRecord, error) {
	relative := "licenses/whisper/WHISPER.CPP-LICENSE"
	destination := filepath.Join(stageRoot, filepath.FromSlash(relative))
	if err := copyRegularFile(filepath.Join(sourceRoot, "LICENSE"), destination, 0o600); err != nil {
		return NoticeRecord{}, err
	}
	digest, size, err := toolchainclosure.DigestFile(destination)
	if err != nil {
		return NoticeRecord{}, err
	}
	return NoticeRecord{
		ID: WhisperLicenseNoticeID, Path: relative, SHA256: digest, ByteSize: size,
	}, nil
}

// stageSourceNotice records the normalized recipe this closure was built from.
// Absolute build paths never reach it: the recorded configuration is already
// normalized to $whisper/$cc/$cxx, so the compiler's install directory is not
// part of the toolchain's identity.
func stageSourceNotice(
	stageRoot string, buildTarget target.Target, backend, compilerIdentity string, values []string,
) (NoticeRecord, error) {
	relative := "licenses/whisper/SOURCE.json"
	destination := filepath.Join(stageRoot, filepath.FromSlash(relative))
	document := struct {
		Schema        int            `json:"schema"`
		Version       string         `json:"version"`
		Target        target.Target  `json:"target"`
		Backend       string         `json:"backend"`
		Sources       []SourceRecord `json:"sources"`
		Compiler      string         `json:"compiler"`
		Configuration []string       `json:"configuration"`
	}{
		Schema: 1, Version: toolchainVersion, Target: buildTarget, Backend: backend,
		Sources: sourceRecords(), Compiler: compilerIdentity, Configuration: values,
	}
	if err := atomicfile.WriteJSON(destination, document, 0o600); err != nil {
		return NoticeRecord{}, err
	}
	digest, size, err := toolchainclosure.DigestFile(destination)
	if err != nil {
		return NoticeRecord{}, err
	}
	return NoticeRecord{
		ID: "whisper-source", Path: relative, SHA256: digest, ByteSize: size,
	}, nil
}

func sortedNotices(values ...NoticeRecord) []NoticeRecord {
	result := append([]NoticeRecord(nil), values...)
	for outer := 1; outer < len(result); outer++ {
		for inner := outer; inner > 0 && result[inner].ID < result[inner-1].ID; inner-- {
			result[inner], result[inner-1] = result[inner-1], result[inner]
		}
	}
	return result
}

// publishStage moves the staged closure next to the API executable. Only this
// closure's own paths are replaced, so the media closure sharing the directory
// is never disturbed.
func publishStage(stageRoot, artifactRoot string) error {
	if err := os.MkdirAll(artifactRoot, 0o755); err != nil {
		return err
	}
	// The fingerprint is removed with everything else it describes, so a fresh
	// closure can never be paired with a stale record of how it was made.
	for _, owned := range []string{
		"whisper", filepath.Join("licenses", "whisper"), ManifestName, buildFingerprintName,
	} {
		if err := os.RemoveAll(filepath.Join(artifactRoot, owned)); err != nil {
			return err
		}
	}
	return filepath.WalkDir(stageRoot, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(stageRoot, name)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		destination := filepath.Join(artifactRoot, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return copyRegularFile(name, destination, info.Mode().Perm())
	})
}

func copyRegularFile(source, destination string, mode os.FileMode) error {
	info, err := os.Lstat(source)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("copy %s: source is not a regular file", filepath.Base(source))
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	_ = os.Remove(destination)
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	if err := output.Sync(); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

func resolveCompilers() (string, string, error) {
	compiler, err := exec.LookPath("cc")
	if err != nil {
		return "", "", fmt.Errorf("whisper toolchain build requires a C compiler: %w", err)
	}
	cxx, err := exec.LookPath("c++")
	if err != nil {
		return "", "", fmt.Errorf("whisper toolchain build requires a C++ compiler: %w", err)
	}
	return compiler, cxx, nil
}

func inspectBuildTools(ctx context.Context, compiler, cxx, cmake string) (string, error) {
	parts := make([]string, 0, 3)
	for _, definition := range []struct{ name, executable string }{
		{"CC", compiler}, {"CXX", cxx}, {"CMAKE", cmake},
	} {
		identity, err := inspectTool(ctx, definition.executable)
		if err != nil {
			return "", fmt.Errorf("inspect %s for whisper build: %w", definition.name, err)
		}
		parts = append(parts, definition.name+":\n"+identity)
	}
	return strings.Join(parts, "\n"), nil
}

func inspectTool(ctx context.Context, executable string) (string, error) {
	command := exec.CommandContext(ctx, executable, "--version")
	output, err := command.CombinedOutput()
	if err != nil || len(output) == 0 || len(output) > 16<<10 {
		return "", fmt.Errorf("inspect whisper build tool")
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	identity := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "InstalledDir:") {
			continue
		}
		identity = append(identity, line)
	}
	if len(identity) == 0 {
		return "", fmt.Errorf("inspect whisper build tool")
	}
	return strings.Join(identity, "\n"), nil
}

// buildFingerprintName records which build logic produced the published
// closure. It sits beside the manifest rather than inside it: it describes how
// the artifacts were made, not what they are, and nothing at runtime consults
// it.
const buildFingerprintName = "whisper-build.fingerprint"

// inspectReuse reports whether the published closure can stand in for a build.
// Any doubt at all is answered with a cold build and a reason, never a partial
// repair.
func inspectReuse(
	repositoryRoot, artifactRoot string, buildTarget target.Target,
) (Verified, bool, string) {
	verified, loadErr := Load(artifactRoot, buildTarget)
	if loadErr != nil {
		return Verified{}, false, fmt.Sprintf("published closure is unusable: %v", loadErr)
	}
	recorded, readErr := os.ReadFile(filepath.Join(artifactRoot, buildFingerprintName))
	if readErr != nil {
		return Verified{}, false, fmt.Sprintf("build fingerprint is unreadable: %v", readErr)
	}
	current, fingerprintErr := buildFingerprint(repositoryRoot)
	if fingerprintErr != nil {
		return Verified{}, false, fmt.Sprintf("build fingerprint is uncomputable: %v", fingerprintErr)
	}
	if strings.TrimSpace(string(recorded)) != current {
		return Verified{}, false, "build logic changed since the closure was built"
	}
	if _, exists := verified.Capabilities[CapabilityLocalTranscriptionV1]; !exists {
		return Verified{}, false, "published closure carries no transcription capability"
	}
	return verified, true, ""
}

// buildFingerprint hashes this closure's build logic wholesale. Over-
// approximating is the right trade here and not in the media toolchain: an
// unrelated edit costs a rebuild measured in seconds rather than the many
// minutes a codec toolchain costs, so precision would buy little and a stale
// list would cost correctness.
func buildFingerprint(repositoryRoot string) (string, error) {
	return toolchainclosure.HashDirectories(
		filepath.Join(repositoryRoot, "internal", "whispertoolchain"),
		filepath.Join(repositoryRoot, "internal", "toolchainclosure"),
	)
}

func writeBuildFingerprint(repositoryRoot, artifactRoot string) error {
	fingerprint, err := buildFingerprint(repositoryRoot)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(artifactRoot, buildFingerprintName), []byte(fingerprint+"\n"), 0o600)
}
