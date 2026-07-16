package mediatoolchain

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestManifestVerifiesContainedDigestBoundCapability(t *testing.T) {
	root := t.TempDir()
	toolPath := filepath.Join(root, "media", target.Host().ExecutableName("ffprobe"))
	if err := os.MkdirAll(filepath.Dir(toolPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toolPath, []byte("fixture-ffprobe"), 0o700); err != nil {
		t.Fatal(err)
	}
	noticePath := filepath.Join(root, "licenses", "media", "LICENSE.md")
	if err := os.MkdirAll(filepath.Dir(noticePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(noticePath, []byte("fixture-license"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest, size, err := digestFile(toolPath)
	if err != nil {
		t.Fatal(err)
	}
	noticeDigest, noticeSize, err := digestFile(noticePath)
	if err != nil {
		t.Fatal(err)
	}
	manifest := fixtureManifest(t, root,
		filepath.ToSlash(filepath.Join("media", filepath.Base(toolPath))), digest, size, noticeDigest, noticeSize,
	)
	if err := atomicfile.WriteJSON(filepath.Join(root, ManifestName), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	verified, err := Load(root, target.Host())
	if err != nil {
		t.Fatal(err)
	}
	physicalToolPath, err := filepath.EvalSymlinks(toolPath)
	if err != nil {
		t.Fatal(err)
	}
	tool, exists := verified.Capabilities[CapabilityProbeV1]
	if !exists || tool.Entry.Path != physicalToolPath || tool.Entry.SHA256 != digest {
		t.Fatalf("verified=%+v", verified)
	}
	frameTool, exists := verified.Capabilities[CapabilityFrameRGBV1]
	if !exists || frameTool.Entry.Path != physicalToolPath || frameTool.Entry.SHA256 != digest {
		t.Fatalf("verified=%+v", verified)
	}

	if err := os.WriteFile(toolPath, []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, target.Host()); err == nil || !strings.Contains(err.Error(), "digest or size mismatch") {
		t.Fatalf("tampered manifest error=%v", err)
	}
}

func TestManifestTreatsSequenceRendererAsOptionalVerifiedCapability(t *testing.T) {
	root := t.TempDir()
	toolPath := filepath.Join(root, "media", target.Host().ExecutableName("open-cut-render"))
	if err := os.MkdirAll(filepath.Dir(toolPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toolPath, []byte("fixture-open-cut-render"), 0o700); err != nil {
		t.Fatal(err)
	}
	noticePath := filepath.Join(root, "licenses", "media", "LICENSE.md")
	if err := os.MkdirAll(filepath.Dir(noticePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(noticePath, []byte("fixture-license"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest, size, err := digestFile(toolPath)
	if err != nil {
		t.Fatal(err)
	}
	noticeDigest, noticeSize, err := digestFile(noticePath)
	if err != nil {
		t.Fatal(err)
	}
	manifest := fixtureManifest(t, root,
		filepath.ToSlash(filepath.Join("media", filepath.Base(toolPath))), digest, size, noticeDigest, noticeSize,
	)
	fontRoot := filepath.Join(root, "media", "fonts", "open-cut-caption-font-v1")
	if err := os.MkdirAll(fontRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	fontPath := filepath.Join(fontRoot, "NotoSans.fixture.ttf")
	if err := os.WriteFile(fontPath, []byte("fixture-noto-font"), 0o600); err != nil {
		t.Fatal(err)
	}
	fontDigest, fontSize, err := digestFile(fontPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Resources = append(manifest.Resources, ResourceRecord{
		ID: "open-cut-caption-font-v1", Kind: ResourceKindFontBundle, Version: "fixture-v1",
		Root:  "media/fonts/open-cut-caption-font-v1",
		Files: []ResourceFileRecord{{Path: "NotoSans.fixture.ttf", SHA256: fontDigest, ByteSize: fontSize}},
	})
	resourceDigest, err := resourceClosureDigest(manifest.Resources[0])
	if err != nil {
		t.Fatal(err)
	}
	manifest.Resources[0].SHA256 = resourceDigest
	evidenceID := conformanceEvidenceNoticeID(CapabilitySequencePreviewRendererV1)
	rendererCapability := CapabilityRecord{
		ID: CapabilitySequencePreviewRendererV1, EntryToolID: "open-cut-render",
		ToolIDs:                     []string{"ffmpeg", "ffprobe", "open-cut-render"},
		ResourceIDs:                 []string{"open-cut-caption-font-v1"},
		NoticeIDs:                   []string{"ffmpeg-license", RendererRelinkNoticeID, evidenceID},
		ConformanceProfile:          ConformanceSequencePreviewV1,
		ConformanceSuiteSHA256:      conformanceSuiteDigest(CapabilitySequencePreviewRendererV1),
		ConformanceEvidenceNoticeID: evidenceID,
	}
	slices.Sort(rendererCapability.NoticeIDs)
	toolByID := make(map[string]ToolRecord, len(manifest.Tools))
	for _, current := range manifest.Tools {
		toolByID[current.ID] = current
	}
	evidence, err := buildConformanceEvidence(
		manifest.Target, rendererCapability, toolByID,
		map[string]ResourceRecord{manifest.Resources[0].ID: manifest.Resources[0]},
		[]ConformanceObservation{{ID: "fixture-matrix", SHA256: "sha256:" + strings.Repeat("e", 64)}},
	)
	if err != nil {
		t.Fatal(err)
	}
	evidenceNotice, err := writeConformanceEvidence(root, evidence)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Notices = append(manifest.Notices, evidenceNotice)
	manifest.Capabilities = append(manifest.Capabilities, rendererCapability)
	if err := bindManifestClosureDigests(&manifest); err != nil {
		t.Fatal(err)
	}
	if err := atomicfile.WriteJSON(filepath.Join(root, ManifestName), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	verified, err := Load(root, target.Host())
	if err != nil {
		t.Fatal(err)
	}
	renderer, exists := verified.Capabilities[CapabilitySequencePreviewRendererV1]
	if !exists || renderer.Entry.SHA256 != digest || renderer.Entry.ID != "open-cut-render" ||
		len(renderer.Resources) != 1 || renderer.Resources[0].SHA256 != manifest.Resources[0].SHA256 {
		t.Fatalf("renderer=%+v exists=%v", renderer, exists)
	}
	extraFont := filepath.Join(fontRoot, "ambient.ttf")
	if err := os.WriteFile(extraFont, []byte("undeclared-font"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, target.Host()); err == nil || !strings.Contains(err.Error(), "undeclared file") {
		t.Fatalf("undeclared resource file error=%v", err)
	}
	if err := os.Remove(extraFont); err != nil {
		t.Fatal(err)
	}
	originalClosure := manifest.Capabilities[len(manifest.Capabilities)-1].ClosureSHA256
	manifest.Capabilities[len(manifest.Capabilities)-1].ClosureSHA256 = "sha256:" + strings.Repeat("0", 64)
	if err := atomicfile.WriteJSON(filepath.Join(root, ManifestName), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, target.Host()); err == nil || !strings.Contains(err.Error(), "closure digest") {
		t.Fatalf("tampered closure digest error=%v", err)
	}
	manifest.Capabilities[len(manifest.Capabilities)-1].ClosureSHA256 = originalClosure
	if err := atomicfile.WriteJSON(filepath.Join(root, ManifestName), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toolPath, []byte("tampered-renderer"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, target.Host()); err == nil || !strings.Contains(err.Error(), "digest or size mismatch") {
		t.Fatalf("tampered renderer error=%v", err)
	}
}

func TestManifestRejectsUnsafeLicenseConfigurationAndLinkedTool(t *testing.T) {
	root := t.TempDir()
	external := filepath.Join(t.TempDir(), "ffprobe")
	if err := os.WriteFile(external, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "media", "ffprobe")
	if err := os.MkdirAll(filepath.Dir(linked), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, linked); err != nil {
		t.Fatal(err)
	}
	noticePath := filepath.Join(root, "licenses", "media", "LICENSE.md")
	if err := os.MkdirAll(filepath.Dir(noticePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(noticePath, []byte("fixture-license"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest, size, err := digestFile(external)
	if err != nil {
		t.Fatal(err)
	}
	noticeDigest, noticeSize, err := digestFile(noticePath)
	if err != nil {
		t.Fatal(err)
	}
	manifest := fixtureManifest(t, root, "media/ffprobe", digest, size, noticeDigest, noticeSize)
	manifest.Build.Configuration = append(manifest.Build.Configuration, "--enable-gpl")
	if err := atomicfile.WriteJSON(filepath.Join(root, ManifestName), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, target.Host()); err == nil || !strings.Contains(err.Error(), "build record") {
		t.Fatalf("unsafe configuration error=%v", err)
	}
	manifest.Build.Configuration = fixtureConfiguration()
	if err := atomicfile.WriteJSON(filepath.Join(root, ManifestName), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, target.Host()); err == nil || !strings.Contains(err.Error(), "linked files") {
		t.Fatalf("linked tool error=%v", err)
	}
}

func fixtureManifest(
	t *testing.T,
	root string,
	toolPath, digest string,
	size uint64,
	noticeDigest string,
	noticeSize uint64,
) Manifest {
	t.Helper()
	manifest := Manifest{
		Schema: ManifestSchema, Target: target.Host(), ToolchainID: "ffmpeg", Version: toolchainVersion,
		LicenseProfile: LicenseProfileLGPL,
		Sources:        mediaSourceRecords(),
		Build: BuildRecord{
			RecipeSHA256: "sha256:" + strings.Repeat("a", 64), Compiler: "fixture-cc 1",
			Configuration:        fixtureConfiguration(),
			WhisperConfiguration: fixtureWhisperConfiguration(target.Host()),
			Renderer:             fixtureRendererBuildRecord(),
		},
		Tools: []ToolRecord{
			{
				ID: "ffprobe", Path: toolPath, SHA256: digest, ByteSize: size,
			},
			{
				ID: "ffmpeg", Path: toolPath, SHA256: digest, ByteSize: size,
			},
			{
				ID: "open-cut-render", Path: toolPath, SHA256: digest, ByteSize: size,
			},
		},
		Resources: []ResourceRecord{},
		Notices: []NoticeRecord{{
			ID: "ffmpeg-license", Path: "licenses/media/LICENSE.md",
			SHA256: noticeDigest, ByteSize: noticeSize,
		}, {
			ID: RendererRelinkNoticeID, Path: "licenses/media/LICENSE.md",
			SHA256: noticeDigest, ByteSize: noticeSize,
		}},
	}
	manifest.Capabilities = baseCapabilityRecords(manifest.Notices)
	toolByID := make(map[string]ToolRecord, len(manifest.Tools))
	for _, current := range manifest.Tools {
		toolByID[current.ID] = current
	}
	for _, capability := range manifest.Capabilities {
		evidence, err := buildConformanceEvidence(
			manifest.Target, capability, toolByID, map[string]ResourceRecord{},
			[]ConformanceObservation{{ID: "fixture-observation", SHA256: "sha256:" + strings.Repeat("f", 64)}},
		)
		if err != nil {
			t.Fatal(err)
		}
		notice, err := writeConformanceEvidence(root, evidence)
		if err != nil {
			t.Fatal(err)
		}
		manifest.Notices = append(manifest.Notices, notice)
	}
	slices.SortFunc(manifest.Notices, func(left, right NoticeRecord) int {
		if left.ID < right.ID {
			return -1
		}
		if left.ID > right.ID {
			return 1
		}
		return 0
	})
	if err := bindManifestClosureDigests(&manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func fixtureRendererBuildRecord() *RendererBuildRecord {
	digest := "sha256:" + strings.Repeat("b", 64)
	return &RendererBuildRecord{
		Schema: 1, ToolID: "open-cut-render", BuildTag: RendererBuildTag,
		GoVersion: "go version go1.25.5 fixture",
		Arguments: []string{
			"build", "-buildvcs=false", "-trimpath", "-mod=readonly", "-tags", RendererBuildTag,
			"-ldflags=-buildid=", "-o", "$output", RendererBuildPackage,
		},
		CFlags: []string{
			"-I$native/include/freetype2", "-I$native/include/fribidi", "-I$harfbuzz/src",
			"-ffile-prefix-map=$source=.",
		},
		LDFlags: []string{"-L$native/lib"}, SourceSHA256: digest, SourceFileCount: 1,
		LinkInputs: []RendererLinkInput{
			{ID: "freetype", Path: "native/lib/libfreetype.a", SHA256: digest, ByteSize: 1},
			{ID: "fribidi", Path: "native/lib/libfribidi.a", SHA256: digest, ByteSize: 1},
			{ID: "harfbuzz", Path: "native/lib/libharfbuzz.a", SHA256: digest, ByteSize: 1},
		},
		RelinkNoticeID:       RendererRelinkNoticeID,
		BaselineRelinkSHA256: digest, BaselineRelinkByteSize: 1,
		ModifiedRelinkSHA256: "sha256:" + strings.Repeat("c", 64), ModifiedRelinkByteSize: 1,
		ModifiedFriBidiSourceSHA256: "sha256:" + strings.Repeat("d", 64),
		SmokeOutputSHA256:           "sha256:" + strings.Repeat("e", 64), SmokeOutputByteSize: 1,
	}
}

func fixtureConfiguration() []string {
	return []string{
		"--disable-gpl", "--disable-nonfree", "--disable-version3", "--disable-network",
		"--disable-protocols", "--enable-protocol=file,pipe,fd", "--disable-demuxer=hls,concat,image2",
		"--enable-libvpx", "--enable-libopus", "--enable-encoder=rawvideo,pcm_s16le,ffv1,libvpx_vp9,libopus",
		"--pkg-config-flags=--static",
		"--enable-muxer=rawvideo,pcm_s16le,wav,webm,matroska",
		"--enable-filter=select,scale,format,transpose,setsar,setparams,setpts,asetpts,aresample,colorspace,pan,aformat",
		"--enable-swresample", "--cc=$cc", "--extra-cflags=-I$deps/include",
		"--extra-ldflags=-L$deps/lib",
	}
}

func fixtureWhisperConfiguration(buildTarget target.Target) []string {
	configuration, err := whisperConfiguration(buildTarget, "$whisper", "$cc", "$cxx")
	if err != nil {
		panic(err)
	}
	return configuration
}

func TestBuildConfigurationNormalizationRemovesEphemeralPaths(t *testing.T) {
	configuration := []string{
		"--cc=/opt/toolchain/bin/cc",
		"--extra-cflags=-I/work/build/dependencies/include -ffile-prefix-map=/work/build=.",
		"--extra-ldflags=-L/work/build/dependencies/lib",
	}
	normalized := normalizeBuildConfiguration(
		configuration, "/work/build", "/work/build/dependencies", "/opt/toolchain/bin/cc",
	)
	want := []string{
		"--cc=$cc",
		"--extra-cflags=-I$deps/include -ffile-prefix-map=$build=.",
		"--extra-ldflags=-L$deps/lib",
	}
	if strings.Join(normalized, "\n") != strings.Join(want, "\n") {
		t.Fatalf("normalized=%q", normalized)
	}
}

func TestWindowsFFmpegConfigurationLinksToolchainRuntimeStatically(t *testing.T) {
	windows := buildConfiguration(
		"$cc", "$build", "$deps", target.Target{Platform: target.Win, Arch: target.X64},
	)
	if !validLGPLConfiguration(windows) ||
		!slices.Contains(windows, "--extra-ldflags=-L$deps/lib -static") {
		t.Fatalf("windows configuration=%q", windows)
	}
	mac := buildConfiguration(
		"$cc", "$build", "$deps", target.Target{Platform: target.Mac, Arch: target.ARM64},
	)
	if slices.Contains(mac, "--extra-ldflags=-L$deps/lib -static") {
		t.Fatalf("mac configuration=%q", mac)
	}
}

func TestLibVPXConfigurationPinsBaselineCPUFeatures(t *testing.T) {
	for _, fixture := range []struct {
		buildTarget target.Target
		disabled    []string
	}{
		{
			buildTarget: target.Target{Platform: target.Mac, Arch: target.ARM64},
			disabled: []string{
				"--disable-neon-dotprod", "--disable-neon-i8mm", "--disable-sve", "--disable-sve2",
			},
		},
		{
			buildTarget: target.Target{Platform: target.Win, Arch: target.X64},
			disabled: []string{
				"--disable-sse3", "--disable-ssse3", "--disable-sse4-1",
				"--disable-avx", "--disable-avx2", "--disable-avx512",
			},
		},
	} {
		configuration, err := libVPXConfiguration("/source", "/prefix", fixture.buildTarget)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(configuration, "--disable-runtime-cpu-detect") ||
			slices.Contains(configuration, "--enable-runtime-cpu-detect") {
			t.Fatalf("target=%s configuration=%q", fixture.buildTarget, configuration)
		}
		for _, required := range fixture.disabled {
			if !slices.Contains(configuration, required) {
				t.Fatalf("target=%s missing=%s configuration=%q", fixture.buildTarget, required, configuration)
			}
		}
	}
}

func TestOpusConfigurationPinsFixedPointBaseline(t *testing.T) {
	configuration := opusConfiguration("/source", "/prefix")
	for _, required := range []string{
		"--enable-fixed-point", "--disable-asm", "--disable-rtcd", "--disable-intrinsics",
	} {
		if !slices.Contains(configuration, required) {
			t.Fatalf("missing=%s configuration=%q", required, configuration)
		}
	}
	if slices.Contains(configuration, "--disable-float-api") {
		t.Fatalf("FFmpeg's libopus wrapper still requires the link-time float API")
	}
}
