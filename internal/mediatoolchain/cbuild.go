package mediatoolchain

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/utils/environment"
	"github.com/PerishCode/open-cut/utils/target"
	"github.com/PerishCode/open-cut/utils/tool"
)

// cbuildProducts is everything the rest of the build needs from the C
// toolchain: the executables it publishes, the static libraries and headers
// the renderer links against, and the configurations the manifest and recipe
// digest record.
type cbuildProducts struct {
	buildRoot           string
	sourceRoot          string
	libVPXRoot          string
	opusRoot            string
	nativeTextRoots     map[string]string
	dependencyRoot      string
	harfBuzzRoot        string
	probe               string
	frameDecoder        string
	configuration       []string
	libVPXConfiguration []string
	opusConfiguration   []string
	nativeText          NativeTextBuildRecipe
	compilerVersion     string
	parallelism         int
	reused              bool
}

// ensureCBuildTree produces the C toolchain, or keeps the one already there.
//
// Nothing in this tree depends on a line of the renderer's Go source, yet a
// renderer edit used to discard all of it, because the build removed the tree
// unconditionally. Relinking the renderer against a preserved tree takes two
// seconds; recompiling the tree to reach the same point takes six minutes.
//
// Reuse is a hint and never an authority. The recorded identity must match
// exactly, every output the later stages consume must still be present, and
// any doubt at all falls back to the full cold build rather than attempting a
// partial repair.
func ensureCBuildTree(
	ctx context.Context,
	repositoryRoot, workspace string,
	archives map[string]string,
	buildTarget target.Target,
	stdout, stderr io.Writer,
) (cbuildProducts, error) {
	buildRoot := filepath.Join(workspace, "build")
	compiler, err := tool.Resolve("cc")
	if err != nil {
		return cbuildProducts{}, err
	}
	shell, err := tool.Resolve("sh")
	if err != nil {
		return cbuildProducts{}, err
	}
	makeTool, err := tool.Resolve("make")
	if err != nil {
		return cbuildProducts{}, err
	}
	cxx, err := tool.Resolve("c++")
	if err != nil {
		return cbuildProducts{}, err
	}
	archiver, err := tool.Resolve("ar")
	if err != nil {
		return cbuildProducts{}, err
	}
	cmake, err := tool.Resolve("cmake")
	if err != nil {
		return cbuildProducts{}, err
	}
	compilerVersion, err := inspectBuildTools(ctx, compiler, cxx, archiver, makeTool, cmake)
	if err != nil {
		return cbuildProducts{}, err
	}
	buildLogic, err := hashDirectories(
		filepath.Join(repositoryRoot, "internal", "mediatoolchain"),
		filepath.Join(repositoryRoot, "internal", "toolchainclosure"),
	)
	if err != nil {
		return cbuildProducts{}, fmt.Errorf("hash media build logic: %w", err)
	}
	identity := cbuildIdentity{
		ToolchainID: "ffmpeg", Version: toolchainVersion, Target: buildTarget.String(),
		CompilerVersion: compilerVersion, BuildLogicSHA256: buildLogic,
	}
	sourceRoot := filepath.Join(buildRoot, "ffmpeg-"+FFmpegSourceVersion)
	dependencyRoot := filepath.Join(buildRoot, "dependencies")
	harfBuzzRoot := filepath.Join(buildRoot, "harfbuzz-"+HarfBuzzSourceVersion)
	parallelism := min(max(runtime.NumCPU(), 1), 16)

	{
		roots := cbuildRoots{
			ffmpeg: sourceRoot, libVPX: filepath.Join(buildRoot, "libvpx-"+LibVPXSourceVersion),
			opus: filepath.Join(buildRoot, "opus-"+OpusSourceVersion), harfBuzz: harfBuzzRoot,
			dependency: dependencyRoot, nativeText: preservedNativeTextRoots(buildRoot),
		}
		material := cbuildReuseMaterial(roots, buildTarget)
		if reusable, reason := reusableCBuildTree(buildRoot, identity, material); reusable {
			stamp, err := readCBuildStamp(buildRoot)
			if err == nil {
				fmt.Fprintln(stderr, "media toolchain C build: reusing the preserved tree")
				preserved := cbuildProducts{
					buildRoot: buildRoot, sourceRoot: sourceRoot, dependencyRoot: dependencyRoot,
					harfBuzzRoot:    harfBuzzRoot,
					libVPXRoot:      filepath.Join(buildRoot, "libvpx-"+LibVPXSourceVersion),
					opusRoot:        filepath.Join(buildRoot, "opus-"+OpusSourceVersion),
					nativeTextRoots: preservedNativeTextRoots(buildRoot),
					probe:           filepath.Join(sourceRoot, buildTarget.ExecutableName("ffprobe")),
					frameDecoder:    filepath.Join(sourceRoot, buildTarget.ExecutableName("ffmpeg")),
					configuration:   stamp.Configuration, libVPXConfiguration: stamp.LibVPXConfiguration,
					opusConfiguration: stamp.OpusConfiguration,
					nativeText:        stamp.NativeText, compilerVersion: stamp.CompilerVersion,
					parallelism: parallelism, reused: true,
				}
				if missing := preserved.incomplete(); missing != "" {
					fmt.Fprintf(stderr, "media toolchain C build: preserved tree cannot supply %s\n", missing)
				} else {
					return preserved, nil
				}
			}
			fmt.Fprintln(stderr, "media toolchain C build: preserved stamp is unreadable")
		} else {
			fmt.Fprintf(stderr, "media toolchain C build: %s\n", reason)
		}
	}

	if err := os.RemoveAll(buildRoot); err != nil {
		return cbuildProducts{}, fmt.Errorf("reset media build root: %w", err)
	}
	if err := os.MkdirAll(buildRoot, 0o700); err != nil {
		return cbuildProducts{}, fmt.Errorf("create media build root: %w", err)
	}
	sourceRoot, err = extractSource(
		archives["ffmpeg"], buildRoot, "ffmpeg-"+FFmpegSourceVersion, "configure",
	)
	if err != nil {
		return cbuildProducts{}, err
	}
	libvpxRoot, err := extractSource(
		archives["libvpx"], buildRoot, "libvpx-"+LibVPXSourceVersion, "configure",
	)
	if err != nil {
		return cbuildProducts{}, err
	}
	opusRoot, err := extractSource(
		archives["libopus"], buildRoot, "opus-"+OpusSourceVersion, "configure",
	)
	if err != nil {
		return cbuildProducts{}, err
	}
	nativeTextRoots, err := extractNativeTextSources(archives, buildRoot)
	if err != nil {
		return cbuildProducts{}, err
	}
	nativeTextRecipe, err := buildStaticNativeTextDependencies(
		ctx, nativeTextRoots, dependencyRoot, compiler, cxx, archiver, shell, makeTool,
		parallelism, stdout, stderr,
	)
	if err != nil {
		return cbuildProducts{}, err
	}
	libvpxConfiguration, err := buildLibVPX(
		ctx, libvpxRoot, dependencyRoot, compiler, shell, makeTool, parallelism, buildTarget, stdout, stderr,
	)
	if err != nil {
		return cbuildProducts{}, err
	}
	opusConfiguration, err := buildOpus(
		ctx, opusRoot, dependencyRoot, compiler, shell, makeTool, parallelism, stdout, stderr,
	)
	if err != nil {
		return cbuildProducts{}, err
	}
	configuration := buildConfiguration(
		shellBuildPath(compiler), shellBuildPath(buildRoot), shellBuildPath(dependencyRoot), buildTarget,
	)
	if !validLGPLConfiguration(configuration) {
		return cbuildProducts{}, fmt.Errorf("generated media configuration violates the LGPL-only profile")
	}
	recordedConfiguration := normalizeBuildConfiguration(configuration, buildRoot, dependencyRoot, compiler)
	recordedLibVPXConfiguration := normalizeBuildConfiguration(
		libvpxConfiguration, buildRoot, dependencyRoot, compiler,
	)
	recordedOpusConfiguration := normalizeBuildConfiguration(
		opusConfiguration, buildRoot, dependencyRoot, compiler,
	)
	buildEnvironment := environment.Merge(os.Environ(), nil, map[string]string{
		"PKG_CONFIG_PATH":   filepath.Join(dependencyRoot, "lib", "pkgconfig"),
		"PKG_CONFIG_LIBDIR": filepath.Join(dependencyRoot, "lib", "pkgconfig"),
	})
	if err := runConfigure(ctx, shell, filepath.Join(sourceRoot, "configure"), configuration,
		sourceRoot, buildEnvironment, stdout, stderr); err != nil {
		return cbuildProducts{}, fmt.Errorf("configure FFmpeg media toolchain: %w", err)
	}
	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: makeTool, Args: []string{
			"-j", fmt.Sprint(parallelism), buildTarget.ExecutableName("ffprobe"), buildTarget.ExecutableName("ffmpeg"),
		},
		Directory: sourceRoot, Env: buildEnvironment, Stdout: stdout, Stderr: stderr,
		Profile: lifecycle.ProfileDevelopment, Presentation: lifecycle.PresentationHeadless,
	}); err != nil {
		return cbuildProducts{}, fmt.Errorf("build FFmpeg media tools: %w", err)
	}
	builtProbe := filepath.Join(sourceRoot, buildTarget.ExecutableName("ffprobe"))
	if info, statErr := os.Stat(builtProbe); statErr != nil || !info.Mode().IsRegular() {
		return cbuildProducts{}, fmt.Errorf("FFmpeg build did not produce ffprobe")
	}
	builtFrameDecoder := filepath.Join(sourceRoot, buildTarget.ExecutableName("ffmpeg"))
	if info, statErr := os.Stat(builtFrameDecoder); statErr != nil || !info.Mode().IsRegular() {
		return cbuildProducts{}, fmt.Errorf("FFmpeg build did not produce ffmpeg")
	}
	for _, current := range []struct{ name, path string }{
		{"ffprobe", builtProbe}, {"ffmpeg", builtFrameDecoder},
	} {
		if err := verifyPackagedExecutableDynamicClosure(current.path); err != nil {
			return cbuildProducts{}, fmt.Errorf("verify %s runtime closure: %w", current.name, err)
		}
	}
	stamp := cbuildStamp{
		Schema: cbuildStampSchema, Identity: identity,
		Configuration: recordedConfiguration, LibVPXConfiguration: recordedLibVPXConfiguration,
		OpusConfiguration: recordedOpusConfiguration,
		NativeText:        nativeTextRecipe, CompilerVersion: compilerVersion,
	}
	if err := writeCBuildStamp(buildRoot, stamp); err != nil {
		return cbuildProducts{}, fmt.Errorf("record C build stamp: %w", err)
	}
	return cbuildProducts{
		buildRoot: buildRoot, sourceRoot: sourceRoot, dependencyRoot: dependencyRoot,
		harfBuzzRoot: harfBuzzRoot, libVPXRoot: libvpxRoot, opusRoot: opusRoot,
		nativeTextRoots: nativeTextRoots,
		probe:           builtProbe, frameDecoder: builtFrameDecoder,
		configuration: recordedConfiguration, libVPXConfiguration: recordedLibVPXConfiguration,
		opusConfiguration: recordedOpusConfiguration,
		nativeText:        nativeTextRecipe, compilerVersion: compilerVersion,
		parallelism: parallelism,
	}, nil
}

// incomplete names the first thing a reused tree failed to supply. The cold
// path fills every field by construction; the reuse path assembles them by
// hand, so an omission there would otherwise surface much later as a confusing
// failure in a stage that had no idea it was handed an empty path.
func (products cbuildProducts) incomplete() string {
	for name, value := range map[string]string{
		"sourceRoot": products.sourceRoot, "dependencyRoot": products.dependencyRoot,
		"harfBuzzRoot": products.harfBuzzRoot, "libVPXRoot": products.libVPXRoot,
		"opusRoot": products.opusRoot,
		"probe":    products.probe, "frameDecoder": products.frameDecoder,
		"compilerVersion": products.compilerVersion,
	} {
		if value == "" {
			return name
		}
	}
	for _, name := range []string{"freetype", "fribidi", "harfbuzz"} {
		if products.nativeTextRoots[name] == "" {
			return "nativeTextRoots[" + name + "]"
		}
	}
	if len(products.configuration) == 0 {
		return "recorded build configuration"
	}
	return ""
}
