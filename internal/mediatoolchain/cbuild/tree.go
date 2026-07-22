package cbuild

import (
	"context"
	"fmt"
	"github.com/PerishCode/open-cut/internal/toolchainclosure"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/utils/environment"
	"github.com/PerishCode/open-cut/utils/target"
)

// Products is everything the rest of the build needs from the C
// toolchain: the executables it publishes, the static libraries and headers
// the renderer links against, and the configurations the manifest and recipe
// digest record.
type Products struct {
	BuildRoot           string
	SourceRoot          string
	LibVPXRoot          string
	OpusRoot            string
	NativeTextRoots     map[string]string
	DependencyRoot      string
	HarfBuzzRoot        string
	Probe               string
	FrameDecoder        string
	Configuration       []string
	LibVPXConfiguration []string
	OpusConfiguration   []string
	NativeText          NativeTextBuildRecipe
	CompilerVersion     string
	Parallelism         int
	Reused              bool
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
// EnsureTree produces the C toolchain, or keeps the one already there.
//
// ToolchainVersion is passed rather than imported: it is the media manifest's
// identity, and this group must not depend on the package that assembles the
// manifest. Everything else the decision compares is derived here.
type Options struct {
	RepositoryRoot   string
	Workspace        string
	Archives         map[string]string
	Target           target.Target
	ToolchainVersion string
	Stdout, Stderr   io.Writer
}

func EnsureTree(ctx context.Context, options Options) (Products, error) {
	repositoryRoot, workspace := options.RepositoryRoot, options.Workspace
	archives, buildTarget := options.Archives, options.Target
	stdout, stderr := options.Stdout, options.Stderr
	buildRoot := filepath.Join(workspace, "build")
	tools, compilerVersion, err := resolveBuildTools(ctx)
	if err != nil {
		return Products{}, err
	}
	compiler, cxx, archiver := tools.compiler, tools.cxx, tools.archiver
	makeTool, shell := tools.makeTool, tools.shell
	// The closure is asked of the compiler, not kept by hand. Hashing the
	// whole parent package was safe but blunt: an edit to manifest assembly,
	// conformance evidence or the renderer - none of which the C compiler ever
	// sees - discarded a tree that costs many minutes to rebuild. Asking
	// `go list -deps` for this package's real closure is stricter in the
	// direction that matters, because a helper this build starts calling is
	// picked up automatically rather than waiting for someone to remember a
	// list.
	buildLogic, err := toolchainclosure.GoSourceClosureFingerprint(
		ctx, repositoryRoot, buildLogicClosureDomain, "", buildLogicPackage,
	)
	if err != nil {
		return Products{}, fmt.Errorf("fingerprint media build logic: %w", err)
	}
	identity := cbuildIdentity{
		ToolchainID: "ffmpeg", Version: options.ToolchainVersion, Target: buildTarget.String(),
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
			dependency: dependencyRoot, NativeText: preservedNativeTextRoots(buildRoot),
		}
		material := cbuildReuseMaterial(roots, buildTarget)
		if reusable, reason := reusableCBuildTree(buildRoot, identity, material); reusable {
			stamp, err := readCBuildStamp(buildRoot)
			if err == nil {
				fmt.Fprintln(stderr, "media toolchain C build: reusing the preserved tree")
				preserved := Products{
					BuildRoot: buildRoot, SourceRoot: sourceRoot, DependencyRoot: dependencyRoot,
					HarfBuzzRoot:    harfBuzzRoot,
					LibVPXRoot:      filepath.Join(buildRoot, "libvpx-"+LibVPXSourceVersion),
					OpusRoot:        filepath.Join(buildRoot, "opus-"+OpusSourceVersion),
					NativeTextRoots: preservedNativeTextRoots(buildRoot),
					Probe:           filepath.Join(sourceRoot, buildTarget.ExecutableName("ffprobe")),
					FrameDecoder:    filepath.Join(sourceRoot, buildTarget.ExecutableName("ffmpeg")),
					Configuration:   stamp.Configuration, LibVPXConfiguration: stamp.LibVPXConfiguration,
					OpusConfiguration: stamp.OpusConfiguration,
					NativeText:        stamp.NativeText, CompilerVersion: stamp.CompilerVersion,
					Parallelism: parallelism, Reused: true,
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
		return Products{}, fmt.Errorf("reset media build root: %w", err)
	}
	if err := os.MkdirAll(buildRoot, 0o700); err != nil {
		return Products{}, fmt.Errorf("create media build root: %w", err)
	}
	sourceRoot, err = extractSource(
		archives["ffmpeg"], buildRoot, "ffmpeg-"+FFmpegSourceVersion, "configure",
	)
	if err != nil {
		return Products{}, err
	}
	libvpxRoot, err := extractSource(
		archives["libvpx"], buildRoot, "libvpx-"+LibVPXSourceVersion, "configure",
	)
	if err != nil {
		return Products{}, err
	}
	OpusRoot, err := extractSource(
		archives["libopus"], buildRoot, "opus-"+OpusSourceVersion, "configure",
	)
	if err != nil {
		return Products{}, err
	}
	NativeTextRoots, err := extractNativeTextSources(archives, buildRoot)
	if err != nil {
		return Products{}, err
	}
	nativeTextRecipe, err := buildStaticNativeTextDependencies(
		ctx, NativeTextRoots, dependencyRoot, compiler, cxx, archiver, shell, makeTool,
		parallelism, stdout, stderr,
	)
	if err != nil {
		return Products{}, err
	}
	libvpxConfiguration, err := buildLibVPX(
		ctx, libvpxRoot, dependencyRoot, compiler, shell, makeTool, parallelism, buildTarget, stdout, stderr,
	)
	if err != nil {
		return Products{}, err
	}
	OpusConfiguration, err := buildOpus(
		ctx, OpusRoot, dependencyRoot, compiler, shell, makeTool, parallelism, stdout, stderr,
	)
	if err != nil {
		return Products{}, err
	}
	Configuration := buildConfiguration(
		shellBuildPath(compiler), shellBuildPath(buildRoot), shellBuildPath(dependencyRoot), buildTarget,
	)
	if !ValidLGPLConfiguration(Configuration) {
		return Products{}, fmt.Errorf("generated media Configuration violates the LGPL-only profile")
	}
	recordedConfiguration := normalizeBuildConfiguration(Configuration, buildRoot, dependencyRoot, compiler)
	recordedLibVPXConfiguration := normalizeBuildConfiguration(
		libvpxConfiguration, buildRoot, dependencyRoot, compiler,
	)
	recordedOpusConfiguration := normalizeBuildConfiguration(
		OpusConfiguration, buildRoot, dependencyRoot, compiler,
	)
	buildEnvironment := environment.Merge(os.Environ(), nil, map[string]string{
		"PKG_CONFIG_PATH":   filepath.Join(dependencyRoot, "lib", "pkgconfig"),
		"PKG_CONFIG_LIBDIR": filepath.Join(dependencyRoot, "lib", "pkgconfig"),
	})
	if err := runConfigure(ctx, shell, filepath.Join(sourceRoot, "configure"), Configuration,
		sourceRoot, buildEnvironment, stdout, stderr); err != nil {
		return Products{}, fmt.Errorf("configure FFmpeg media toolchain: %w", err)
	}
	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: makeTool, Args: []string{
			"-j", fmt.Sprint(parallelism), buildTarget.ExecutableName("ffprobe"), buildTarget.ExecutableName("ffmpeg"),
		},
		Directory: sourceRoot, Env: buildEnvironment, Stdout: stdout, Stderr: stderr,
		Profile: lifecycle.ProfileDevelopment, Presentation: lifecycle.PresentationHeadless,
	}); err != nil {
		return Products{}, fmt.Errorf("build FFmpeg media tools: %w", err)
	}
	builtProbe := filepath.Join(sourceRoot, buildTarget.ExecutableName("ffprobe"))
	if info, statErr := os.Stat(builtProbe); statErr != nil || !info.Mode().IsRegular() {
		return Products{}, fmt.Errorf("FFmpeg build did not produce ffprobe")
	}
	builtFrameDecoder := filepath.Join(sourceRoot, buildTarget.ExecutableName("ffmpeg"))
	if info, statErr := os.Stat(builtFrameDecoder); statErr != nil || !info.Mode().IsRegular() {
		return Products{}, fmt.Errorf("FFmpeg build did not produce ffmpeg")
	}
	for _, current := range []struct{ name, path string }{
		{"ffprobe", builtProbe}, {"ffmpeg", builtFrameDecoder},
	} {
		if err := verifyPackagedExecutableDynamicClosure(current.path); err != nil {
			return Products{}, fmt.Errorf("verify %s runtime closure: %w", current.name, err)
		}
	}
	stamp := cbuildStamp{
		Schema: cbuildStampSchema, Identity: identity,
		Configuration: recordedConfiguration, LibVPXConfiguration: recordedLibVPXConfiguration,
		OpusConfiguration: recordedOpusConfiguration,
		NativeText:        nativeTextRecipe, CompilerVersion: compilerVersion,
	}
	if err := writeCBuildStamp(buildRoot, stamp); err != nil {
		return Products{}, fmt.Errorf("record C build stamp: %w", err)
	}
	return Products{
		BuildRoot: buildRoot, SourceRoot: sourceRoot, DependencyRoot: dependencyRoot,
		HarfBuzzRoot: harfBuzzRoot, LibVPXRoot: libvpxRoot, OpusRoot: OpusRoot,
		NativeTextRoots: NativeTextRoots,
		Probe:           builtProbe, FrameDecoder: builtFrameDecoder,
		Configuration: recordedConfiguration, LibVPXConfiguration: recordedLibVPXConfiguration,
		OpusConfiguration: recordedOpusConfiguration,
		NativeText:        nativeTextRecipe, CompilerVersion: compilerVersion,
		Parallelism: parallelism,
	}, nil
}

// incomplete names the first thing a Reused tree failed to supply. The cold
// path fills every field by construction; the reuse path assembles them by
// hand, so an omission there would otherwise surface much later as a confusing
// failure in a stage that had no idea it was handed an empty path.
func (products Products) incomplete() string {
	for name, value := range map[string]string{
		"sourceRoot": products.SourceRoot, "dependencyRoot": products.DependencyRoot,
		"harfBuzzRoot": products.HarfBuzzRoot, "LibVPXRoot": products.LibVPXRoot,
		"OpusRoot": products.OpusRoot,
		"Probe":    products.Probe, "FrameDecoder": products.FrameDecoder,
		"CompilerVersion": products.CompilerVersion,
	} {
		if value == "" {
			return name
		}
	}
	for _, name := range []string{"freetype", "fribidi", "harfbuzz"} {
		if products.NativeTextRoots[name] == "" {
			return "NativeTextRoots[" + name + "]"
		}
	}
	if len(products.Configuration) == 0 {
		return "recorded build Configuration"
	}
	return ""
}
