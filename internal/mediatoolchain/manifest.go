package mediatoolchain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/PerishCode/open-cut/internal/toolchainclosure"
	"github.com/PerishCode/open-cut/utils/target"
)

const (
	ManifestSchema                      = 5
	ManifestName                        = "media-tools.json"
	LicenseProfileLGPL                  = "lgpl-static+ftl+mit+bsd+ofl-v1"
	ResourceKindFontBundle              = "font-bundle"
	CapabilityProbeV1                   = "probe-v1"
	CapabilityFrameRGBV1                = "frame-rgb24-v1"
	CapabilitySourceProxyV1             = "source-proxy-webm-vp9-opus-v1"
	CapabilityRenderInputV1             = "render-input-matroska-ffv1-pcm-v1"
	CapabilitySequencePreviewRendererV1 = "sequence-preview-renderer-v1"
	CapabilitySequenceExportRendererV1  = "sequence-export-renderer-v1"
	ConformanceProbeV1                  = "probe-v1"
	ConformanceFrameRGBV1               = "frame-rgb24-v1"
	ConformanceSourceProxyV1            = "source-proxy-webm-vp9-opus-v1"
	ConformanceRenderInputV1            = "render-input-matroska-ffv1-pcm-v1"
	ConformanceSequencePreviewV1        = "sequence-preview-renderer-v1"
	ConformanceSequenceExportV1         = "sequence-export-renderer-v1"
	FFmpegSourceVersion                 = "8.1.2"
	FFmpegSourceURL                     = "https://ffmpeg.org/releases/ffmpeg-8.1.2.tar.gz"
	FFmpegSignatureURL                  = "https://ffmpeg.org/releases/ffmpeg-8.1.2.tar.gz.asc"
	FFmpegSourceSHA256                  = "sha256:32faba5ef67340d54724941eae1425580791195312a4fd13bf6f820a2818bf22"
	LibVPXSourceVersion                 = "1.16.0"
	LibVPXSourceURL                     = "https://github.com/webmproject/libvpx/archive/v1.16.0/libvpx-1.16.0.tar.gz"
	LibVPXSourceSHA256                  = "sha256:7a479a3c66b9f5d5542a4c6a1b7d3768a983b1e5c14c60a9396edc9b649e015c"
	OpusSourceVersion                   = "1.6.1"
	OpusSourceURL                       = "https://downloads.xiph.org/releases/opus/opus-1.6.1.tar.gz"
	OpusSourceSHA256                    = "sha256:6ffcb593207be92584df15b32466ed64bbec99109f007c82205f0194572411a1"
	maximumManifestBytes                = 256 << 10
)

// capabilityClosureDomain separates media capability identities from every
// other toolchain closure the API owns.
const capabilityClosureDomain = "open-cut/media-capability-closure/v1"

var (
	ErrUnavailable = errors.New("media toolchain is unavailable")
	identifier     = toolchainclosure.Identifier
)

type Manifest struct {
	Schema         int                `json:"schema"`
	Target         target.Target      `json:"target"`
	ToolchainID    string             `json:"toolchainId"`
	Version        string             `json:"version"`
	LicenseProfile string             `json:"licenseProfile"`
	Sources        []SourceRecord     `json:"sources"`
	Build          BuildRecord        `json:"build"`
	Tools          []ToolRecord       `json:"tools"`
	Resources      []ResourceRecord   `json:"resources"`
	Capabilities   []CapabilityRecord `json:"capabilities"`
	Notices        []NoticeRecord     `json:"notices"`
}

// The record and projection shapes are the shared closure mechanism; the media
// toolchain owns its schema, identity gates and determinism contract, not the
// machinery that proves declared bytes.
type SourceRecord = toolchainclosure.SourceRecord

type BuildRecord struct {
	RecipeSHA256  string               `json:"recipeSha256"`
	Compiler      string               `json:"compiler"`
	Configuration []string             `json:"configuration"`
	Renderer      *RendererBuildRecord `json:"renderer"`
}

type ToolRecord = toolchainclosure.ToolRecord

type ResourceRecord = toolchainclosure.ResourceRecord

type ResourceFileRecord = toolchainclosure.ResourceFileRecord

type CapabilityRecord = toolchainclosure.CapabilityRecord

type NoticeRecord = toolchainclosure.NoticeRecord

type Tool = toolchainclosure.Tool

type ResourceFile = toolchainclosure.ResourceFile

type Resource = toolchainclosure.Resource

type Capability = toolchainclosure.Capability

type Verified struct {
	Manifest     Manifest
	Root         string
	Tools        map[string]Tool
	Resources    map[string]Resource
	Capabilities map[string]Capability
}

func LoadForExecutable(executable string, expected target.Target) (Verified, error) {
	if !cleanAbsolute(executable) {
		return Verified{}, fmt.Errorf("media toolchain executable root is invalid")
	}
	physical, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return Verified{}, fmt.Errorf("resolve API executable: %w", err)
	}
	return Load(filepath.Dir(physical), expected)
}

func Load(root string, expected target.Target) (Verified, error) {
	manifest, root, err := readManifest(root, expected)
	if err != nil {
		return Verified{}, err
	}
	contents, err := toolchainclosure.Verify(root, expected, "media toolchain", toolchainclosure.Declaration{
		Tools:        manifest.Tools,
		Resources:    manifest.Resources,
		Notices:      manifest.Notices,
		Capabilities: manifest.Capabilities,
	})
	if err != nil {
		return Verified{}, err
	}
	return Verified{
		Manifest: manifest, Root: root, Tools: contents.Tools,
		Resources: contents.Resources, Capabilities: contents.Capabilities,
	}, nil
}

func readManifest(root string, expected target.Target) (Manifest, string, error) {
	if !cleanAbsolute(root) || expected.Validate() != nil {
		return Manifest{}, "", fmt.Errorf("media toolchain root or target is invalid")
	}
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil || !cleanAbsolute(physicalRoot) {
		return Manifest{}, "", fmt.Errorf("resolve media toolchain root")
	}
	root = physicalRoot
	manifestPath := filepath.Join(root, ManifestName)
	info, err := os.Lstat(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, "", ErrUnavailable
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > maximumManifestBytes {
		return Manifest{}, "", fmt.Errorf("media toolchain manifest is invalid")
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return Manifest{}, "", fmt.Errorf("read media toolchain manifest: %w", err)
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return Manifest{}, "", fmt.Errorf("decode media toolchain manifest")
	}
	if err := validateManifest(manifest, expected); err != nil {
		return Manifest{}, "", err
	}
	return manifest, root, nil
}

func validateManifest(manifest Manifest, expected target.Target) error {
	if manifest.Schema != ManifestSchema || manifest.Target != expected ||
		!identifier.MatchString(manifest.ToolchainID) || manifest.Version != toolchainVersion ||
		manifest.LicenseProfile != LicenseProfileLGPL || len(manifest.Tools) == 0 || len(manifest.Tools) > 16 ||
		len(manifest.Sources) == 0 || len(manifest.Sources) > 32 ||
		len(manifest.Resources) > 16 || len(manifest.Capabilities) < 3 || len(manifest.Capabilities) > 16 ||
		len(manifest.Notices) == 0 || len(manifest.Notices) > 64 {
		return fmt.Errorf("media toolchain manifest identity is invalid")
	}
	if !slices.Equal(manifest.Sources, mediaSourceRecords()) {
		return fmt.Errorf("media toolchain source record is invalid")
	}
	if !validDigest(manifest.Build.RecipeSHA256) || strings.TrimSpace(manifest.Build.Compiler) == "" ||
		len(manifest.Build.Compiler) > 4096 || !validLGPLConfiguration(manifest.Build.Configuration) ||
		!validRecordedConfiguration(manifest.Build.Configuration) ||
		validateRendererBuildRecord(manifest.Build.Renderer, manifest.Target) != nil {
		return fmt.Errorf("media toolchain build record is invalid")
	}

	tools := make(map[string]ToolRecord, len(manifest.Tools))
	for _, tool := range manifest.Tools {
		if !identifier.MatchString(tool.ID) || !validRelative(tool.Path) || !validDigest(tool.SHA256) ||
			tool.ByteSize == 0 {
			return fmt.Errorf("media toolchain tool record is invalid")
		}
		if _, duplicate := tools[tool.ID]; duplicate {
			return fmt.Errorf("media toolchain repeats a tool identity")
		}
		tools[tool.ID] = tool
	}
	if _, exists := tools[manifest.Build.Renderer.ToolID]; !exists {
		return fmt.Errorf("media toolchain renderer build tool is unavailable")
	}

	resources := make(map[string]ResourceRecord, len(manifest.Resources))
	for _, resource := range manifest.Resources {
		if !identifier.MatchString(resource.ID) ||
			resource.Kind != ResourceKindFontBundle ||
			strings.TrimSpace(resource.Version) != resource.Version || resource.Version == "" ||
			len(resource.Version) > 128 || !validRelative(resource.Root) || !validDigest(resource.SHA256) ||
			len(resource.Files) == 0 || len(resource.Files) > 128 {
			return fmt.Errorf("media toolchain resource record is invalid")
		}
		if _, duplicate := resources[resource.ID]; duplicate {
			return fmt.Errorf("media toolchain repeats a resource identity")
		}
		previous := ""
		for _, file := range resource.Files {
			if !validRelative(file.Path) || file.Path <= previous || !validDigest(file.SHA256) || file.ByteSize == 0 {
				return fmt.Errorf("media toolchain resource file record is invalid")
			}
			previous = file.Path
		}
		if digest, err := resourceClosureDigest(resource); err != nil || digest != resource.SHA256 {
			return fmt.Errorf("media toolchain resource closure digest is invalid")
		}
		resources[resource.ID] = resource
	}

	notices := make(map[string]NoticeRecord, len(manifest.Notices))
	for _, notice := range manifest.Notices {
		if !identifier.MatchString(notice.ID) || !validRelative(notice.Path) ||
			!validDigest(notice.SHA256) || notice.ByteSize == 0 {
			return fmt.Errorf("media toolchain notice record is invalid")
		}
		if _, duplicate := notices[notice.ID]; duplicate {
			return fmt.Errorf("media toolchain repeats a notice")
		}
		notices[notice.ID] = notice
	}
	if _, exists := notices[manifest.Build.Renderer.RelinkNoticeID]; !exists {
		return fmt.Errorf("media toolchain renderer relink notice is unavailable")
	}

	capabilities := make(map[string]struct{}, len(manifest.Capabilities))
	for _, capability := range manifest.Capabilities {
		expectedProfile, supported := capabilityConformanceProfile(capability.ID)
		if !supported || capability.ConformanceProfile != expectedProfile ||
			!validDigest(capability.ConformanceSuiteSHA256) ||
			capability.ConformanceSuiteSHA256 != conformanceSuiteDigest(capability.ID) ||
			!identifier.MatchString(capability.ConformanceEvidenceNoticeID) ||
			!identifier.MatchString(capability.EntryToolID) || !validDigest(capability.ClosureSHA256) ||
			len(capability.ToolIDs) == 0 || len(capability.ToolIDs) > 16 ||
			len(capability.ResourceIDs) > 16 || len(capability.NoticeIDs) == 0 || len(capability.NoticeIDs) > 64 ||
			!strictlySortedUnique(capability.ToolIDs) ||
			!strictlySortedUnique(capability.ResourceIDs) ||
			!strictlySortedUnique(capability.NoticeIDs) {
			return fmt.Errorf("media toolchain capability record is invalid")
		}
		if _, duplicate := capabilities[capability.ID]; duplicate {
			return fmt.Errorf("media toolchain repeats a capability")
		}
		entry, exists := tools[capability.EntryToolID]
		if !exists || !slices.Contains(capability.ToolIDs, entry.ID) {
			return fmt.Errorf("media toolchain capability entry is outside its closure")
		}
		for _, id := range capability.ToolIDs {
			if _, exists := tools[id]; !exists {
				return fmt.Errorf("media toolchain capability references an unknown tool")
			}
		}
		for _, id := range capability.ResourceIDs {
			if _, exists := resources[id]; !exists {
				return fmt.Errorf("media toolchain capability references an unknown resource")
			}
		}
		for _, id := range capability.NoticeIDs {
			if _, exists := notices[id]; !exists {
				return fmt.Errorf("media toolchain capability references an unknown notice")
			}
		}
		if !slices.Contains(capability.NoticeIDs, capability.ConformanceEvidenceNoticeID) {
			return fmt.Errorf("media toolchain capability evidence is outside its closure")
		}
		if err := validateCapabilityShape(capability, resources); err != nil {
			return err
		}
		if digest, err := capabilityClosureDigest(capability, tools, resources, notices); err != nil ||
			digest != capability.ClosureSHA256 {
			return fmt.Errorf("media toolchain capability closure digest is invalid")
		}
		capabilities[capability.ID] = struct{}{}
	}
	for _, required := range []string{
		CapabilityProbeV1, CapabilityFrameRGBV1, CapabilitySourceProxyV1, CapabilityRenderInputV1,
	} {
		if _, exists := capabilities[required]; !exists {
			return fmt.Errorf("media toolchain is missing required capability %s", required)
		}
	}
	return nil
}

func validateCapabilityShape(capability CapabilityRecord, resources map[string]ResourceRecord) error {
	switch capability.ID {
	case CapabilityProbeV1:
		if capability.EntryToolID != "ffprobe" || !slices.Equal(capability.ToolIDs, []string{"ffprobe"}) ||
			len(capability.ResourceIDs) != 0 {
			return fmt.Errorf("probe capability closure is invalid")
		}
	case CapabilityFrameRGBV1, CapabilitySourceProxyV1, CapabilityRenderInputV1:
		if capability.EntryToolID != "ffmpeg" || !slices.Equal(capability.ToolIDs, []string{"ffmpeg"}) ||
			len(capability.ResourceIDs) != 0 {
			return fmt.Errorf("FFmpeg capability closure is invalid")
		}
	case CapabilitySequencePreviewRendererV1, CapabilitySequenceExportRendererV1:
		if capability.EntryToolID != "open-cut-render" ||
			!slices.Equal(capability.ToolIDs, []string{"ffmpeg", "ffprobe", "open-cut-render"}) ||
			len(capability.ResourceIDs) != 1 ||
			resources[capability.ResourceIDs[0]].Kind != ResourceKindFontBundle ||
			!slices.Contains(capability.NoticeIDs, RendererRelinkNoticeID) {
			return fmt.Errorf("sequence preview capability closure is invalid")
		}
	default:
		return fmt.Errorf("media toolchain capability is unsupported")
	}
	return nil
}

func capabilityConformanceProfile(id string) (string, bool) {
	switch id {
	case CapabilityProbeV1:
		return ConformanceProbeV1, true
	case CapabilityFrameRGBV1:
		return ConformanceFrameRGBV1, true
	case CapabilitySourceProxyV1:
		return ConformanceSourceProxyV1, true
	case CapabilityRenderInputV1:
		return ConformanceRenderInputV1, true
	case CapabilitySequencePreviewRendererV1, CapabilitySequenceExportRendererV1:
		return id, true
	default:
		return "", false
	}
}

func strictlySortedUnique(values []string) bool {
	return toolchainclosure.StrictlySortedUnique(values)
}

func resourceClosureDigest(record ResourceRecord) (string, error) {
	return toolchainclosure.ResourceClosureDigest(record)
}

func capabilityClosureDigest(
	record CapabilityRecord,
	tools map[string]ToolRecord,
	resources map[string]ResourceRecord,
	notices map[string]NoticeRecord,
) (string, error) {
	return toolchainclosure.CapabilityClosureDigest(
		capabilityClosureDomain, record, tools, resources, notices,
	)
}

func bindManifestClosureDigests(manifest *Manifest) error {
	if manifest == nil {
		return fmt.Errorf("media manifest is required")
	}
	for index := range manifest.Resources {
		digest, err := resourceClosureDigest(manifest.Resources[index])
		if err != nil {
			return err
		}
		manifest.Resources[index].SHA256 = digest
	}
	tools := make(map[string]ToolRecord, len(manifest.Tools))
	for _, tool := range manifest.Tools {
		tools[tool.ID] = tool
	}
	resources := make(map[string]ResourceRecord, len(manifest.Resources))
	for _, resource := range manifest.Resources {
		resources[resource.ID] = resource
	}
	notices := make(map[string]NoticeRecord, len(manifest.Notices))
	for _, notice := range manifest.Notices {
		notices[notice.ID] = notice
	}
	for index := range manifest.Capabilities {
		digest, err := capabilityClosureDigest(manifest.Capabilities[index], tools, resources, notices)
		if err != nil {
			return err
		}
		manifest.Capabilities[index].ClosureSHA256 = digest
	}
	return nil
}

func closureDigest(domain string, value any) (string, error) {
	return toolchainclosure.ClosureDigest(domain, value)
}

func mediaSourceRecords() []SourceRecord {
	result := []SourceRecord{
		{
			ID: "ffmpeg", Version: FFmpegSourceVersion, URL: FFmpegSourceURL,
			SignatureURL: FFmpegSignatureURL, SHA256: FFmpegSourceSHA256, License: "LGPL-2.1-or-later",
		},
		{
			ID: "libvpx", Version: LibVPXSourceVersion, URL: LibVPXSourceURL,
			SHA256: LibVPXSourceSHA256, License: "BSD-3-Clause",
		},
		{
			ID: "libopus", Version: OpusSourceVersion, URL: OpusSourceURL,
			SHA256: OpusSourceSHA256, License: "BSD-3-Clause",
		},
	}
	result = append(result, nativeTextSourceRecords()...)
	return append(result, captionFontSourceRecords()...)
}

func validLGPLConfiguration(configuration []string) bool {
	if len(configuration) == 0 || len(configuration) > 256 ||
		!slices.Contains(configuration, "--disable-gpl") ||
		!slices.Contains(configuration, "--disable-nonfree") ||
		!slices.Contains(configuration, "--disable-version3") ||
		!slices.Contains(configuration, "--disable-network") ||
		!slices.Contains(configuration, "--disable-protocols") ||
		!slices.Contains(configuration, "--enable-protocol=file,pipe,fd") ||
		!slices.Contains(configuration, "--disable-demuxer=hls,concat,image2") ||
		!slices.Contains(configuration, "--enable-libvpx") ||
		!slices.Contains(configuration, "--enable-libopus") ||
		!slices.Contains(configuration, "--pkg-config-flags=--static") ||
		!slices.Contains(configuration, "--enable-encoder=rawvideo,pcm_s16le,ffv1,libvpx_vp9,libopus") ||
		!slices.Contains(configuration, "--enable-muxer=rawvideo,pcm_s16le,wav,webm,matroska") ||
		!slices.Contains(configuration, "--enable-filter=select,scale,format,transpose,setsar,setparams,setpts,asetpts,aresample,colorspace,pan,aformat") ||
		!slices.Contains(configuration, "--enable-swresample") {
		return false
	}
	for _, value := range configuration {
		lower := strings.ToLower(value)
		if value == "" || len(value) > 1024 || lower == "--enable-gpl" || lower == "--enable-nonfree" ||
			strings.Contains(lower, "libx264") || strings.Contains(lower, "libx265") {
			return false
		}
	}
	return true
}

func validRecordedConfiguration(configuration []string) bool {
	for _, value := range configuration {
		if strings.Contains(value, "--cc=/") || strings.Contains(value, "-I/") ||
			strings.Contains(value, "-L/") || strings.Contains(value, ":\\") {
			return false
		}
	}
	return true
}

func resolveContainedDirectory(root, relative string) (string, error) {
	return toolchainclosure.ResolveContainedDirectory(root, relative)
}

func verifyResourceTree(root string, declared []ResourceFileRecord) error {
	return toolchainclosure.VerifyResourceTree(root, declared)
}

func resolveContainedRegular(root, relative string) (string, error) {
	return toolchainclosure.ResolveContainedRegular(root, relative)
}

func digestFile(filename string) (string, uint64, error) {
	return toolchainclosure.DigestFile(filename)
}

func validRelative(value string) bool {
	return toolchainclosure.ValidRelative(value)
}

func validDigest(value string) bool {
	return toolchainclosure.ValidDigest(value)
}

func cleanAbsolute(value string) bool {
	return toolchainclosure.CleanAbsolute(value)
}
