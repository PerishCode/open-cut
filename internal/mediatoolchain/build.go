package mediatoolchain

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/environment"
	"github.com/PerishCode/open-cut/utils/target"
	"github.com/PerishCode/open-cut/utils/tool"
)

const (
	toolchainVersion = "ffmpeg-8.1.2-whisper-1.8.6-open-cut.15"
)

type BuildOptions struct {
	RepositoryRoot string
	Target         target.Target
	Stdout         io.Writer
	Stderr         io.Writer
}

type BuildResult struct {
	Schema            int      `json:"schema"`
	Target            string   `json:"target"`
	Version           string   `json:"version"`
	Manifest          string   `json:"manifest"`
	Probe             string   `json:"probe"`
	ProbeSHA256       string   `json:"probeSha256"`
	FrameDecoder      string   `json:"frameDecoder"`
	FrameSHA256       string   `json:"frameSha256"`
	ProxyEncoder      string   `json:"proxyEncoder"`
	ProxySHA256       string   `json:"proxySha256"`
	Renderer          string   `json:"renderer"`
	RendererSHA256    string   `json:"rendererSha256"`
	Transcriber       string   `json:"transcriber"`
	TranscriberSHA256 string   `json:"transcriberSha256"`
	SourceSHA256      []string `json:"sourceSha256"`
	RecipeSHA256      string   `json:"recipeSha256"`
	Reused            bool     `json:"reused"`
}

func Build(ctx context.Context, options BuildOptions) (BuildResult, error) {
	repositoryRoot, err := filepath.Abs(options.RepositoryRoot)
	if err != nil {
		return BuildResult{}, err
	}
	repositoryRoot, err = filepath.EvalSymlinks(repositoryRoot)
	if err != nil || !repositoryMarker(repositoryRoot) {
		return BuildResult{}, fmt.Errorf("media toolchain build requires an open-cut repository root")
	}
	if options.Target.Validate() != nil || options.Target != target.Host() {
		return BuildResult{}, fmt.Errorf("media toolchain source build requires the host public target")
	}
	stdout, stderr := options.Stdout, options.Stderr
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	artifactRoot := filepath.Join(repositoryRoot, "apps", "api", "dist", "sidecar")
	if verified, loadErr := Load(artifactRoot, options.Target); loadErr == nil {
		if err := VerifyCapabilities(ctx, verified); err != nil {
			return BuildResult{}, fmt.Errorf("verify reused media capabilities: %w", err)
		}
		probe := verified.Capabilities[CapabilityProbeV1].Entry
		decoder := verified.Capabilities[CapabilityFrameRGBV1].Entry
		proxy := verified.Capabilities[CapabilitySourceProxyV1].Entry
		renderer := verified.Tools["open-cut-render"]
		transcriber := verified.Tools["whisper-cli"]
		return BuildResult{
			Schema: 1, Target: options.Target.String(), Version: verified.Manifest.Version,
			Manifest: filepath.Join(artifactRoot, ManifestName), Probe: probe.Path,
			ProbeSHA256: probe.SHA256, FrameDecoder: decoder.Path, FrameSHA256: decoder.SHA256,
			ProxyEncoder: proxy.Path, ProxySHA256: proxy.SHA256,
			Renderer: renderer.Path, RendererSHA256: renderer.SHA256,
			Transcriber: transcriber.Path, TranscriberSHA256: transcriber.SHA256,
			SourceSHA256: sourceDigests(verified.Manifest.Sources),
			RecipeSHA256: verified.Manifest.Build.RecipeSHA256, Reused: true,
		}, nil
	}
	workspace := filepath.Join(repositoryRoot, ".tmp", "oc-control", "media-toolchain", options.Target.String())
	sources := mediaSourceRecords()
	archives := make(map[string]string, len(sources))
	for _, source := range sources {
		archive, err := sourceArchivePath(workspace, source)
		if err != nil {
			return BuildResult{}, err
		}
		if err := ensureSource(ctx, archive, source); err != nil {
			return BuildResult{}, err
		}
		archives[source.ID] = archive
	}
	buildRoot := filepath.Join(workspace, "build")
	if err := os.RemoveAll(buildRoot); err != nil {
		return BuildResult{}, fmt.Errorf("reset media build root: %w", err)
	}
	if err := os.MkdirAll(buildRoot, 0o700); err != nil {
		return BuildResult{}, fmt.Errorf("create media build root: %w", err)
	}
	sourceRoot, err := extractSource(
		archives["ffmpeg"], buildRoot, "ffmpeg-"+FFmpegSourceVersion, "configure",
	)
	if err != nil {
		return BuildResult{}, err
	}
	libvpxRoot, err := extractSource(
		archives["libvpx"], buildRoot, "libvpx-"+LibVPXSourceVersion, "configure",
	)
	if err != nil {
		return BuildResult{}, err
	}
	opusRoot, err := extractSource(
		archives["libopus"], buildRoot, "opus-"+OpusSourceVersion, "configure",
	)
	if err != nil {
		return BuildResult{}, err
	}
	whisperRoot, err := extractWhisperSource(archives["whisper.cpp"], buildRoot)
	if err != nil {
		return BuildResult{}, err
	}
	nativeTextRoots, err := extractNativeTextSources(archives, buildRoot)
	if err != nil {
		return BuildResult{}, err
	}
	compiler, err := tool.Resolve("cc")
	if err != nil {
		return BuildResult{}, err
	}
	shell, err := tool.Resolve("sh")
	if err != nil {
		return BuildResult{}, err
	}
	makeTool, err := tool.Resolve("make")
	if err != nil {
		return BuildResult{}, err
	}
	cxx, err := tool.Resolve("c++")
	if err != nil {
		return BuildResult{}, err
	}
	archiver, err := tool.Resolve("ar")
	if err != nil {
		return BuildResult{}, err
	}
	cmake, err := tool.Resolve("cmake")
	if err != nil {
		return BuildResult{}, err
	}
	compilerVersion, err := inspectBuildTools(ctx, compiler, cxx, archiver, makeTool, cmake)
	if err != nil {
		return BuildResult{}, err
	}
	dependencyRoot := filepath.Join(buildRoot, "dependencies")
	parallelism := runtime.NumCPU()
	if parallelism < 1 {
		parallelism = 1
	}
	if parallelism > 16 {
		parallelism = 16
	}
	nativeTextRecipe, err := buildStaticNativeTextDependencies(
		ctx, nativeTextRoots, dependencyRoot, compiler, cxx, archiver, shell, makeTool,
		parallelism, stdout, stderr,
	)
	if err != nil {
		return BuildResult{}, err
	}
	libvpxConfiguration, err := buildLibVPX(
		ctx, libvpxRoot, dependencyRoot, compiler, shell, makeTool, parallelism, options.Target, stdout, stderr,
	)
	if err != nil {
		return BuildResult{}, err
	}
	opusConfiguration, err := buildOpus(
		ctx, opusRoot, dependencyRoot, compiler, shell, makeTool, parallelism, stdout, stderr,
	)
	if err != nil {
		return BuildResult{}, err
	}
	configuration := buildConfiguration(
		shellBuildPath(compiler), shellBuildPath(buildRoot), shellBuildPath(dependencyRoot),
	)
	if !validLGPLConfiguration(configuration) {
		return BuildResult{}, fmt.Errorf("generated media configuration violates the LGPL-only profile")
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
	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: shell, Args: append([]string{filepath.Join(sourceRoot, "configure")}, configuration...),
		Directory: sourceRoot, Env: buildEnvironment, Stdout: stdout, Stderr: stderr,
		Profile: lifecycle.ProfileDevelopment, Presentation: lifecycle.PresentationHeadless,
	}); err != nil {
		return BuildResult{}, fmt.Errorf("configure FFmpeg media toolchain: %w", err)
	}
	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: makeTool, Args: []string{"-j", fmt.Sprint(parallelism), "ffprobe", "ffmpeg"},
		Directory: sourceRoot, Env: buildEnvironment, Stdout: stdout, Stderr: stderr,
		Profile: lifecycle.ProfileDevelopment, Presentation: lifecycle.PresentationHeadless,
	}); err != nil {
		return BuildResult{}, fmt.Errorf("build FFmpeg media tools: %w", err)
	}
	builtProbe := filepath.Join(sourceRoot, options.Target.ExecutableName("ffprobe"))
	if info, statErr := os.Stat(builtProbe); statErr != nil || !info.Mode().IsRegular() {
		return BuildResult{}, fmt.Errorf("FFmpeg build did not produce ffprobe")
	}
	builtFrameDecoder := filepath.Join(sourceRoot, options.Target.ExecutableName("ffmpeg"))
	if info, statErr := os.Stat(builtFrameDecoder); statErr != nil || !info.Mode().IsRegular() {
		return BuildResult{}, fmt.Errorf("FFmpeg build did not produce ffmpeg")
	}
	builtWhisper, recordedWhisperConfiguration, err := buildWhisperCLI(
		ctx, whisperRoot, buildRoot, cmake, compiler, cxx, parallelism, options.Target, stdout, stderr,
	)
	if err != nil {
		return BuildResult{}, err
	}
	stageRoot := filepath.Join(workspace, "stage")
	if err := os.RemoveAll(stageRoot); err != nil {
		return BuildResult{}, err
	}
	probeRelative := filepath.ToSlash(filepath.Join("media", options.Target.ExecutableName("ffprobe")))
	probePath := filepath.Join(stageRoot, filepath.FromSlash(probeRelative))
	if err := copyRegularFile(builtProbe, probePath, 0o755); err != nil {
		return BuildResult{}, err
	}
	frameRelative := filepath.ToSlash(filepath.Join("media", options.Target.ExecutableName("ffmpeg")))
	framePath := filepath.Join(stageRoot, filepath.FromSlash(frameRelative))
	if err := copyRegularFile(builtFrameDecoder, framePath, 0o755); err != nil {
		return BuildResult{}, err
	}
	whisperRelative := filepath.ToSlash(filepath.Join("media", options.Target.ExecutableName("whisper-cli")))
	whisperPath := filepath.Join(stageRoot, filepath.FromSlash(whisperRelative))
	if err := copyRegularFile(builtWhisper, whisperPath, 0o755); err != nil {
		return BuildResult{}, err
	}
	fontResource, err := stageCaptionFontBundle(archives, stageRoot)
	if err != nil {
		return BuildResult{}, err
	}
	whisperResource, err := stageWhisperConformanceModel(whisperRoot, stageRoot)
	if err != nil {
		return BuildResult{}, err
	}
	rendererTool, rendererNotice, rendererRecord, err := buildAndStageRenderer(
		ctx, repositoryRoot, buildRoot, dependencyRoot, nativeTextRoots["harfbuzz"], stageRoot,
		options.Target, archives, framePath, fontResource, stdout, stderr,
	)
	if err != nil {
		return BuildResult{}, err
	}
	baseNotices, err := stageNotices(
		sourceRoot, libvpxRoot, opusRoot, stageRoot, compilerVersion,
		recordedConfiguration, recordedLibVPXConfiguration, recordedOpusConfiguration,
		recordedWhisperConfiguration,
		nativeTextRecipe, rendererRecord, options.Target,
	)
	if err != nil {
		return BuildResult{}, err
	}
	whisperNotice, err := stageWhisperNotice(whisperRoot, stageRoot)
	if err != nil {
		return BuildResult{}, err
	}
	fontNotices, err := stageCaptionFontNotices(archives, stageRoot)
	if err != nil {
		return BuildResult{}, err
	}
	nativeTextNotices, err := stageNativeTextNotices(nativeTextRoots, stageRoot)
	if err != nil {
		return BuildResult{}, err
	}
	notices := append(append([]NoticeRecord(nil), baseNotices...), fontNotices...)
	notices = append(notices, nativeTextNotices...)
	notices = append(notices, rendererNotice)
	notices = append(notices, whisperNotice)
	probeDigest, probeSize, err := digestFile(probePath)
	if err != nil {
		return BuildResult{}, err
	}
	frameDigest, frameSize, err := digestFile(framePath)
	if err != nil {
		return BuildResult{}, err
	}
	whisperDigest, whisperSize, err := digestFile(whisperPath)
	if err != nil {
		return BuildResult{}, err
	}
	recipeDigest, err := digestRecipe(
		options.Target, compilerVersion,
		recordedConfiguration, recordedLibVPXConfiguration, recordedOpusConfiguration,
		recordedWhisperConfiguration,
		nativeTextRecipe, rendererRecord,
	)
	if err != nil {
		return BuildResult{}, err
	}
	toolRecords := []ToolRecord{
		{ID: "ffprobe", Path: probeRelative, SHA256: probeDigest, ByteSize: probeSize},
		{ID: "ffmpeg", Path: frameRelative, SHA256: frameDigest, ByteSize: frameSize},
		rendererTool,
		{ID: "whisper-cli", Path: whisperRelative, SHA256: whisperDigest, ByteSize: whisperSize},
	}
	baseCapabilities := baseCapabilityRecords(baseNotices)
	evidenceNotices, err := stageBaseConformanceEvidence(
		ctx, options.Target, stageRoot, toolRecords, baseCapabilities, probePath, framePath,
	)
	if err != nil {
		return BuildResult{}, err
	}
	notices = append(notices, evidenceNotices...)
	rendererCapabilities := make([]CapabilityRecord, 0, 2)
	for _, capabilityID := range []string{
		CapabilitySequencePreviewRendererV1, CapabilitySequenceExportRendererV1,
	} {
		capability := rendererCapabilityRecord(capabilityID, notices, fontResource)
		evidence, evidenceErr := stageRendererConformanceEvidence(
			ctx, options.Target, stageRoot, toolRecords, fontResource, capability,
		)
		if evidenceErr != nil {
			return BuildResult{}, fmt.Errorf("qualify staged %s: %w", capabilityID, evidenceErr)
		}
		notices = append(notices, evidence)
		rendererCapabilities = append(rendererCapabilities, capability)
	}
	transcriptionCapability := localTranscriptionCapabilityRecord(baseNotices, whisperNotice, whisperResource)
	transcriptionEvidence, err := stageLocalTranscriptionConformanceEvidence(
		ctx, options.Target, stageRoot, toolRecords, whisperResource, transcriptionCapability,
	)
	if err != nil {
		return BuildResult{}, fmt.Errorf("qualify staged local transcription capability: %w", err)
	}
	notices = append(notices, transcriptionEvidence)
	capabilityRecords := append(baseCapabilities, rendererCapabilities...)
	capabilityRecords = append(capabilityRecords, transcriptionCapability)
	manifest := Manifest{
		Schema: ManifestSchema, Target: options.Target, ToolchainID: "ffmpeg", Version: toolchainVersion,
		LicenseProfile: LicenseProfileLGPL,
		Sources:        sources,
		Build: BuildRecord{
			RecipeSHA256: recipeDigest, Compiler: compilerVersion,
			Configuration:        append([]string(nil), recordedConfiguration...),
			WhisperConfiguration: append([]string(nil), recordedWhisperConfiguration...),
			Renderer:             &rendererRecord,
		},
		Tools:        toolRecords,
		Resources:    []ResourceRecord{fontResource, whisperResource},
		Capabilities: capabilityRecords,
		Notices:      notices,
	}
	if err := bindManifestClosureDigests(&manifest); err != nil {
		return BuildResult{}, err
	}
	if err := atomicfile.WriteJSON(filepath.Join(stageRoot, ManifestName), manifest, 0o600); err != nil {
		return BuildResult{}, err
	}
	staged, err := Load(stageRoot, options.Target)
	if err != nil {
		return BuildResult{}, fmt.Errorf("verify staged media toolchain: %w", err)
	}
	if err := VerifyCapabilities(ctx, staged); err != nil {
		return BuildResult{}, fmt.Errorf("verify staged media capabilities: %w", err)
	}
	if err := publishStage(stageRoot, artifactRoot, staged.Manifest); err != nil {
		return BuildResult{}, err
	}
	verified, err := Load(artifactRoot, options.Target)
	if err != nil {
		return BuildResult{}, fmt.Errorf("verify published media toolchain: %w", err)
	}
	probe := verified.Capabilities[CapabilityProbeV1].Entry
	decoder := verified.Capabilities[CapabilityFrameRGBV1].Entry
	proxy := verified.Capabilities[CapabilitySourceProxyV1].Entry
	renderer := verified.Tools["open-cut-render"]
	transcriber := verified.Tools["whisper-cli"]
	return BuildResult{
		Schema: 1, Target: options.Target.String(), Version: manifest.Version,
		Manifest: filepath.Join(artifactRoot, ManifestName), Probe: probe.Path,
		ProbeSHA256: probe.SHA256, FrameDecoder: decoder.Path, FrameSHA256: decoder.SHA256,
		ProxyEncoder: proxy.Path, ProxySHA256: proxy.SHA256,
		Renderer: renderer.Path, RendererSHA256: renderer.SHA256,
		Transcriber: transcriber.Path, TranscriberSHA256: transcriber.SHA256,
		SourceSHA256: sourceDigests(manifest.Sources), RecipeSHA256: manifest.Build.RecipeSHA256,
	}, nil
}

func buildConfiguration(compiler, buildRoot, dependencyRoot string) []string {
	return []string{
		"--disable-gpl", "--disable-nonfree", "--disable-version3", "--disable-network",
		"--disable-protocols", "--enable-protocol=file,pipe,fd", "--disable-demuxer=hls,concat,image2",
		"--disable-autodetect", "--disable-doc", "--disable-debug", "--enable-ffmpeg", "--disable-ffplay",
		"--enable-ffprobe", "--disable-avdevice", "--enable-libvpx", "--enable-libopus",
		"--pkg-config-flags=--static",
		"--disable-encoders", "--enable-encoder=rawvideo,pcm_s16le,ffv1,libvpx_vp9,libopus",
		"--disable-muxers", "--enable-muxer=rawvideo,pcm_s16le,wav,webm,matroska", "--disable-filters",
		"--enable-filter=select,scale,format,transpose,setsar,setparams,setpts,asetpts,aresample,colorspace,pan,aformat",
		"--enable-swscale", "--enable-swresample",
		"--disable-asm", "--disable-stripping", "--cc=" + compiler,
		"--extra-cflags=-I" + filepath.Join(dependencyRoot, "include") + " -ffile-prefix-map=" + buildRoot + "=.",
		"--extra-ldflags=-L" + filepath.Join(dependencyRoot, "lib"),
	}
}

func normalizeBuildConfiguration(
	configuration []string,
	buildRoot string,
	dependencyRoot string,
	compiler string,
) []string {
	result := make([]string, len(configuration))
	replacements := []struct{ actual, token string }{
		{dependencyRoot, "$deps"},
		{shellBuildPath(dependencyRoot), "$deps"},
		{buildRoot, "$build"},
		{shellBuildPath(buildRoot), "$build"},
		{compiler, "$cc"},
		{shellBuildPath(compiler), "$cc"},
	}
	for index, value := range configuration {
		for _, replacement := range replacements {
			value = strings.ReplaceAll(value, replacement.actual, replacement.token)
		}
		result[index] = value
	}
	return result
}

func buildLibVPX(
	ctx context.Context,
	sourceRoot, prefix, compiler, shell, makeTool string,
	parallelism int,
	buildTarget target.Target,
	stdout, stderr io.Writer,
) ([]string, error) {
	configuration, err := libVPXConfiguration(shellBuildPath(sourceRoot), shellBuildPath(prefix), buildTarget)
	if err != nil {
		return nil, err
	}
	buildEnvironment := environment.Merge(os.Environ(), nil, map[string]string{"CC": shellBuildPath(compiler)})
	if err := runConfigure(
		ctx, shell, filepath.Join(sourceRoot, "configure"), configuration,
		sourceRoot, buildEnvironment, stdout, stderr,
	); err != nil {
		return nil, fmt.Errorf("configure pinned libvpx: %w", err)
	}
	for _, arguments := range [][]string{{"-j", fmt.Sprint(parallelism)}, {"install"}} {
		if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
			Executable: makeTool, Args: arguments, Directory: sourceRoot,
			Env: buildEnvironment, Stdout: stdout, Stderr: stderr,
			Profile: lifecycle.ProfileDevelopment, Presentation: lifecycle.PresentationHeadless,
		}); err != nil {
			return nil, fmt.Errorf("build pinned libvpx: %w", err)
		}
	}
	return configuration, nil
}

func libVPXTarget(buildTarget target.Target) (string, error) {
	switch buildTarget {
	case target.Target{Platform: target.Mac, Arch: target.ARM64}:
		return "arm64-darwin23-gcc", nil
	case target.Target{Platform: target.Mac, Arch: target.X64}:
		return "x86_64-darwin17-gcc", nil
	case target.Target{Platform: target.Linux, Arch: target.ARM64}:
		return "arm64-linux-gcc", nil
	case target.Target{Platform: target.Linux, Arch: target.X64}:
		return "x86_64-linux-gcc", nil
	case target.Target{Platform: target.Win, Arch: target.ARM64}:
		return "arm64-win64-gcc", nil
	case target.Target{Platform: target.Win, Arch: target.X64}:
		return "x86_64-win64-gcc", nil
	default:
		return "", fmt.Errorf("libvpx target is unsupported")
	}
}

func buildOpus(
	ctx context.Context,
	sourceRoot, prefix, compiler, shell, makeTool string,
	parallelism int,
	stdout, stderr io.Writer,
) ([]string, error) {
	configuration := opusConfiguration(shellBuildPath(sourceRoot), shellBuildPath(prefix))
	buildEnvironment := environment.Merge(os.Environ(), nil, map[string]string{
		"CC":     shellBuildPath(compiler),
		"CFLAGS": "-O2 -ffile-prefix-map=" + shellBuildPath(sourceRoot) + "=.",
	})
	if err := runConfigure(
		ctx, shell, filepath.Join(sourceRoot, "configure"), configuration,
		sourceRoot, buildEnvironment, stdout, stderr,
	); err != nil {
		return nil, fmt.Errorf("configure pinned libopus: %w", err)
	}
	for _, arguments := range [][]string{{"-j", fmt.Sprint(parallelism)}, {"install"}} {
		if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
			Executable: makeTool, Args: arguments, Directory: sourceRoot,
			Env: buildEnvironment, Stdout: stdout, Stderr: stderr,
			Profile: lifecycle.ProfileDevelopment, Presentation: lifecycle.PresentationHeadless,
		}); err != nil {
			return nil, fmt.Errorf("build pinned libopus: %w", err)
		}
	}
	return configuration, nil
}

func opusConfiguration(sourceRoot, prefix string) []string {
	return []string{
		"--prefix=" + prefix, "--disable-shared", "--enable-static", "--disable-doc",
		"--disable-extra-programs", "--enable-fixed-point", "--disable-asm", "--disable-rtcd", "--disable-intrinsics",
		"--disable-dependency-tracking",
	}
}

func sourceDigests(sources []SourceRecord) []string {
	result := make([]string, len(sources))
	for index, source := range sources {
		result[index] = source.ID + "@" + source.SHA256
	}
	return result
}

func baseCapabilityRecords(notices []NoticeRecord) []CapabilityRecord {
	noticeIDs := make([]string, len(notices))
	for index, notice := range notices {
		noticeIDs[index] = notice.ID
	}
	slices.Sort(noticeIDs)
	records := []CapabilityRecord{
		{
			ID: CapabilityFrameRGBV1, EntryToolID: "ffmpeg", ToolIDs: []string{"ffmpeg"},
			ResourceIDs:        []string{},
			ConformanceProfile: ConformanceFrameRGBV1,
		},
		{
			ID: CapabilityProbeV1, EntryToolID: "ffprobe", ToolIDs: []string{"ffprobe"},
			ResourceIDs:        []string{},
			ConformanceProfile: ConformanceProbeV1,
		},
		{
			ID: CapabilitySourceProxyV1, EntryToolID: "ffmpeg", ToolIDs: []string{"ffmpeg"},
			ResourceIDs:        []string{},
			ConformanceProfile: ConformanceSourceProxyV1,
		},
		{
			ID: CapabilityRenderInputV1, EntryToolID: "ffmpeg", ToolIDs: []string{"ffmpeg"},
			ResourceIDs:        []string{},
			ConformanceProfile: ConformanceRenderInputV1,
		},
	}
	for index := range records {
		evidenceID := conformanceEvidenceNoticeID(records[index].ID)
		records[index].NoticeIDs = append(append([]string(nil), noticeIDs...), evidenceID)
		slices.Sort(records[index].NoticeIDs)
		records[index].ConformanceSuiteSHA256 = conformanceSuiteDigest(records[index].ID)
		records[index].ConformanceEvidenceNoticeID = evidenceID
	}
	return records
}

func stageBaseConformanceEvidence(
	ctx context.Context,
	buildTarget target.Target,
	stageRoot string,
	tools []ToolRecord,
	capabilities []CapabilityRecord,
	probePath string,
	ffmpegPath string,
) ([]NoticeRecord, error) {
	observations, err := qualifyBaseCapabilities(ctx, probePath, ffmpegPath, ffmpegPath, ffmpegPath)
	if err != nil {
		return nil, fmt.Errorf("qualify staged media capabilities: %w", err)
	}
	toolByID := make(map[string]ToolRecord, len(tools))
	for _, current := range tools {
		toolByID[current.ID] = current
	}
	result := make([]NoticeRecord, 0, len(capabilities))
	for _, capability := range capabilities {
		evidence, buildErr := buildConformanceEvidence(
			buildTarget, capability, toolByID, map[string]ResourceRecord{}, observations[capability.ID],
		)
		if buildErr != nil {
			return nil, buildErr
		}
		notice, writeErr := writeConformanceEvidence(stageRoot, evidence)
		if writeErr != nil {
			return nil, writeErr
		}
		result = append(result, notice)
	}
	return result, nil
}

func ensureSource(ctx context.Context, archive string, source SourceRecord) error {
	if digest, _, err := digestFile(archive); err == nil && digest == source.SHA256 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(archive), 0o700); err != nil {
		return fmt.Errorf("create media source cache: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(archive), ".media-source-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		temporary.Close()
		return err
	}
	request.Header.Set("User-Agent", "open-cut-media-toolchain/1")
	client := &http.Client{Timeout: 5 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		temporary.Close()
		return fmt.Errorf("download pinned %s source: %w", source.ID, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > maximumSourceBytes {
		temporary.Close()
		return fmt.Errorf("download pinned %s source: unexpected response", source.ID)
	}
	written, copyErr := io.Copy(temporary, io.LimitReader(response.Body, maximumSourceBytes+1))
	if copyErr != nil || written <= 0 || written > maximumSourceBytes {
		temporary.Close()
		return fmt.Errorf("download pinned %s source: invalid body", source.ID)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	digest, _, err := digestFile(temporaryPath)
	if err != nil || digest != source.SHA256 {
		return fmt.Errorf("pinned %s source digest mismatch", source.ID)
	}
	if err := os.Rename(temporaryPath, archive); err != nil {
		return fmt.Errorf("publish media source cache: %w", err)
	}
	return nil
}

func stageNotices(
	ffmpegRoot, libvpxRoot, opusRoot, stageRoot, compiler string,
	ffmpegConfiguration, libvpxConfiguration, opusConfiguration, whisperConfiguration []string,
	nativeText NativeTextBuildRecipe,
	renderer RendererBuildRecord,
	buildTarget target.Target,
) ([]NoticeRecord, error) {
	noticeDefinitions := []struct{ id, source, relative string }{
		{"ffmpeg-license", filepath.Join(ffmpegRoot, "LICENSE.md"), "licenses/media/FFMPEG-LICENSE.md"},
		{"ffmpeg-lgpl-2.1", filepath.Join(ffmpegRoot, "COPYING.LGPLv2.1"), "licenses/media/COPYING.LGPLv2.1"},
		{"libvpx-license", filepath.Join(libvpxRoot, "LICENSE"), "licenses/media/LIBVPX-LICENSE"},
		{"libvpx-patents", filepath.Join(libvpxRoot, "PATENTS"), "licenses/media/LIBVPX-PATENTS"},
		{"libopus-license", filepath.Join(opusRoot, "COPYING"), "licenses/media/LIBOPUS-LICENSE"},
	}
	result := make([]NoticeRecord, 0, len(noticeDefinitions)+1)
	for _, definition := range noticeDefinitions {
		destination := filepath.Join(stageRoot, filepath.FromSlash(definition.relative))
		if err := copyRegularFile(definition.source, destination, 0o600); err != nil {
			return nil, err
		}
		digest, size, err := digestFile(destination)
		if err != nil {
			return nil, err
		}
		result = append(result, NoticeRecord{ID: definition.id, Path: definition.relative, SHA256: digest, ByteSize: size})
	}
	sourceRecord := struct {
		Schema                int                           `json:"schema"`
		Target                target.Target                 `json:"target"`
		Sources               []SourceRecord                `json:"sources"`
		Compiler              string                        `json:"compiler"`
		FFmpegConfiguration   []string                      `json:"ffmpegConfiguration"`
		LibVPXConfiguration   []string                      `json:"libvpxConfiguration"`
		OpusConfiguration     []string                      `json:"opusConfiguration"`
		WhisperConfiguration  []string                      `json:"whisperConfiguration"`
		CaptionFontSelections []captionFontArchiveSelection `json:"captionFontSelections"`
		NativeText            NativeTextBuildRecipe         `json:"nativeText"`
		Renderer              RendererBuildRecord           `json:"renderer"`
	}{
		Schema: 5, Target: buildTarget,
		Sources: mediaSourceRecords(), Compiler: compiler,
		FFmpegConfiguration:   ffmpegConfiguration,
		LibVPXConfiguration:   libvpxConfiguration,
		OpusConfiguration:     opusConfiguration,
		WhisperConfiguration:  whisperConfiguration,
		CaptionFontSelections: captionFontSelections(),
		NativeText:            nativeText,
		Renderer:              renderer,
	}
	relative := "licenses/media/SOURCE.json"
	filename := filepath.Join(stageRoot, filepath.FromSlash(relative))
	if err := atomicfile.WriteJSON(filename, sourceRecord, 0o600); err != nil {
		return nil, err
	}
	digest, size, err := digestFile(filename)
	if err != nil {
		return nil, err
	}
	return append(result, NoticeRecord{ID: "media-source", Path: relative, SHA256: digest, ByteSize: size}), nil
}

func publishStage(stageRoot, artifactRoot string, manifest Manifest) error {
	if err := os.MkdirAll(artifactRoot, 0o700); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(artifactRoot, ManifestName))
	type publicationFile struct {
		relative string
		mode     os.FileMode
	}
	files := make([]publicationFile, 0, len(manifest.Tools)+len(manifest.Notices))
	for _, tool := range manifest.Tools {
		files = append(files, publicationFile{relative: tool.Path, mode: 0o755})
	}
	for _, resource := range manifest.Resources {
		for _, file := range resource.Files {
			files = append(files, publicationFile{relative: path.Join(resource.Root, file.Path), mode: 0o600})
		}
	}
	for _, notice := range manifest.Notices {
		files = append(files, publicationFile{relative: notice.Path, mode: 0o600})
	}
	for _, file := range files {
		if err := copyRegularFile(
			filepath.Join(stageRoot, filepath.FromSlash(file.relative)),
			filepath.Join(artifactRoot, filepath.FromSlash(file.relative)), file.mode,
		); err != nil {
			return err
		}
	}
	manifestBytes, err := os.ReadFile(filepath.Join(stageRoot, ManifestName))
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(artifactRoot, ManifestName), manifestBytes, 0o600)
}

func copyRegularFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("media toolchain source file is not regular")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".media-stage-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := io.Copy(temporary, input); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, destination)
}
