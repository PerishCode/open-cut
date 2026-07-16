package mediatoolchain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/PerishCode/open-cut/internal/mediaclosure"
	"github.com/PerishCode/open-cut/utils/target"
)

const (
	ManifestSchema                            = 4
	ManifestName                              = "media-tools.json"
	LicenseProfileLGPL                        = "lgpl-static+ftl+mit+bsd+ofl-v1"
	ResourceKindFontBundle                    = "font-bundle"
	ResourceKindTranscriptionConformanceModel = "transcription-conformance-model"
	CapabilityProbeV1                         = "probe-v1"
	CapabilityFrameRGBV1                      = "frame-rgb24-v1"
	CapabilitySourceProxyV1                   = "source-proxy-webm-vp9-opus-v1"
	CapabilityRenderInputV1                   = "render-input-matroska-ffv1-pcm-v1"
	CapabilitySequencePreviewRendererV1       = "sequence-preview-renderer-v1"
	CapabilitySequenceExportRendererV1        = "sequence-export-renderer-v1"
	CapabilityLocalTranscriptionV1            = "local-transcription-v1"
	ConformanceProbeV1                        = "probe-v1"
	ConformanceFrameRGBV1                     = "frame-rgb24-v1"
	ConformanceSourceProxyV1                  = "source-proxy-webm-vp9-opus-v1"
	ConformanceRenderInputV1                  = "render-input-matroska-ffv1-pcm-v1"
	ConformanceSequencePreviewV1              = "sequence-preview-renderer-v1"
	ConformanceSequenceExportV1               = "sequence-export-renderer-v1"
	ConformanceLocalTranscriptionV1           = "local-transcription-v1"
	FFmpegSourceVersion                       = "8.1.2"
	FFmpegSourceURL                           = "https://ffmpeg.org/releases/ffmpeg-8.1.2.tar.gz"
	FFmpegSignatureURL                        = "https://ffmpeg.org/releases/ffmpeg-8.1.2.tar.gz.asc"
	FFmpegSourceSHA256                        = "sha256:32faba5ef67340d54724941eae1425580791195312a4fd13bf6f820a2818bf22"
	LibVPXSourceVersion                       = "1.16.0"
	LibVPXSourceURL                           = "https://github.com/webmproject/libvpx/archive/v1.16.0/libvpx-1.16.0.tar.gz"
	LibVPXSourceSHA256                        = "sha256:7a479a3c66b9f5d5542a4c6a1b7d3768a983b1e5c14c60a9396edc9b649e015c"
	OpusSourceVersion                         = "1.6.1"
	OpusSourceURL                             = "https://downloads.xiph.org/releases/opus/opus-1.6.1.tar.gz"
	OpusSourceSHA256                          = "sha256:6ffcb593207be92584df15b32466ed64bbec99109f007c82205f0194572411a1"
	maximumManifestBytes                      = 256 << 10
)

var (
	ErrUnavailable = errors.New("media toolchain is unavailable")
	identifier     = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,63}$`)
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

type SourceRecord struct {
	ID           string `json:"id"`
	Version      string `json:"version"`
	URL          string `json:"url"`
	SignatureURL string `json:"signatureUrl,omitempty"`
	SHA256       string `json:"sha256"`
	License      string `json:"license"`
}

type BuildRecord struct {
	RecipeSHA256         string               `json:"recipeSha256"`
	Compiler             string               `json:"compiler"`
	Configuration        []string             `json:"configuration"`
	WhisperConfiguration []string             `json:"whisperConfiguration"`
	Renderer             *RendererBuildRecord `json:"renderer"`
}

type ToolRecord struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	ByteSize uint64 `json:"byteSize"`
}

type ResourceRecord struct {
	ID      string               `json:"id"`
	Kind    string               `json:"kind"`
	Version string               `json:"version"`
	Root    string               `json:"root"`
	SHA256  string               `json:"sha256"`
	Files   []ResourceFileRecord `json:"files"`
}

type ResourceFileRecord struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	ByteSize uint64 `json:"byteSize"`
}

type CapabilityRecord struct {
	ID                          string   `json:"id"`
	EntryToolID                 string   `json:"entryToolId"`
	ToolIDs                     []string `json:"toolIds"`
	ResourceIDs                 []string `json:"resourceIds"`
	NoticeIDs                   []string `json:"noticeIds"`
	ConformanceProfile          string   `json:"conformanceProfile"`
	ConformanceSuiteSHA256      string   `json:"conformanceSuiteSha256"`
	ConformanceEvidenceNoticeID string   `json:"conformanceEvidenceNoticeId"`
	ClosureSHA256               string   `json:"closureSha256"`
}

type NoticeRecord struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	ByteSize uint64 `json:"byteSize"`
}

type Tool struct {
	ID       string
	Path     string
	SHA256   string
	ByteSize uint64
}

type ResourceFile struct {
	Path     string
	SHA256   string
	ByteSize uint64
}

type Resource struct {
	ID      string
	Kind    string
	Version string
	Root    string
	SHA256  string
	Files   []ResourceFile
}

type Capability struct {
	ID                     string
	Entry                  Tool
	Tools                  []Tool
	Resources              []Resource
	Notices                []NoticeRecord
	ConformanceProfile     string
	ConformanceSuiteSHA256 string
	ConformanceEvidence    NoticeRecord
	ClosureSHA256          string
}

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
	tools := make(map[string]Tool, len(manifest.Tools))
	for _, record := range manifest.Tools {
		filename, err := resolveContainedRegular(root, record.Path)
		if err != nil {
			return Verified{}, fmt.Errorf("validate media tool %s: %w", record.ID, err)
		}
		actualDigest, actualSize, err := digestFile(filename)
		if err != nil || actualDigest != record.SHA256 || actualSize != record.ByteSize {
			return Verified{}, fmt.Errorf("validate media tool %s: digest or size mismatch", record.ID)
		}
		if expected.Platform != target.Win {
			toolInfo, statErr := os.Stat(filename)
			if statErr != nil || toolInfo.Mode().Perm()&0o111 == 0 {
				return Verified{}, fmt.Errorf("validate media tool %s: executable bit is unavailable", record.ID)
			}
		}
		tools[record.ID] = Tool{ID: record.ID, Path: filename, SHA256: record.SHA256, ByteSize: record.ByteSize}
	}
	resources := make(map[string]Resource, len(manifest.Resources))
	for _, record := range manifest.Resources {
		resourceRoot, err := resolveContainedDirectory(root, record.Root)
		if err != nil {
			return Verified{}, fmt.Errorf("validate media resource %s: %w", record.ID, err)
		}
		files := make([]ResourceFile, 0, len(record.Files))
		for _, declared := range record.Files {
			relative := path.Join(record.Root, declared.Path)
			filename, err := resolveContainedRegular(root, relative)
			if err != nil {
				return Verified{}, fmt.Errorf("validate media resource %s file %s: %w", record.ID, declared.Path, err)
			}
			actualDigest, actualSize, err := digestFile(filename)
			if err != nil || actualDigest != declared.SHA256 || actualSize != declared.ByteSize {
				return Verified{}, fmt.Errorf("validate media resource %s file %s: digest or size mismatch", record.ID, declared.Path)
			}
			files = append(files, ResourceFile{
				Path: filename, SHA256: declared.SHA256, ByteSize: declared.ByteSize,
			})
		}
		if err := verifyResourceTree(resourceRoot, record.Files); err != nil {
			return Verified{}, fmt.Errorf("validate media resource %s: %w", record.ID, err)
		}
		resources[record.ID] = Resource{
			ID: record.ID, Kind: record.Kind, Version: record.Version, Root: resourceRoot,
			SHA256: record.SHA256, Files: files,
		}
	}
	notices := make(map[string]NoticeRecord, len(manifest.Notices))
	for _, notice := range manifest.Notices {
		filename, err := resolveContainedRegular(root, notice.Path)
		if err != nil {
			return Verified{}, fmt.Errorf("validate media notice %s: %w", notice.ID, err)
		}
		actualDigest, actualSize, err := digestFile(filename)
		if err != nil || actualDigest != notice.SHA256 || actualSize != notice.ByteSize {
			return Verified{}, fmt.Errorf("validate media notice %s: digest or size mismatch", notice.ID)
		}
		notices[notice.ID] = notice
	}
	capabilities := make(map[string]Capability, len(manifest.Capabilities))
	for _, record := range manifest.Capabilities {
		capability := Capability{
			ID: record.ID, Entry: tools[record.EntryToolID],
			Tools:                  make([]Tool, 0, len(record.ToolIDs)),
			Resources:              make([]Resource, 0, len(record.ResourceIDs)),
			Notices:                make([]NoticeRecord, 0, len(record.NoticeIDs)),
			ConformanceProfile:     record.ConformanceProfile,
			ConformanceSuiteSHA256: record.ConformanceSuiteSHA256,
			ConformanceEvidence:    notices[record.ConformanceEvidenceNoticeID],
			ClosureSHA256:          record.ClosureSHA256,
		}
		for _, id := range record.ToolIDs {
			capability.Tools = append(capability.Tools, tools[id])
		}
		for _, id := range record.ResourceIDs {
			capability.Resources = append(capability.Resources, resources[id])
		}
		for _, id := range record.NoticeIDs {
			capability.Notices = append(capability.Notices, notices[id])
		}
		capabilities[record.ID] = capability
	}
	return Verified{
		Manifest: manifest, Root: root, Tools: tools, Resources: resources, Capabilities: capabilities,
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
		!validWhisperConfiguration(manifest.Build.WhisperConfiguration, manifest.Target) ||
		!validRecordedConfiguration(manifest.Build.WhisperConfiguration) ||
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
			(resource.Kind != ResourceKindFontBundle && resource.Kind != ResourceKindTranscriptionConformanceModel) ||
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
	case CapabilityLocalTranscriptionV1:
		if capability.EntryToolID != "whisper-cli" ||
			!slices.Equal(capability.ToolIDs, []string{"ffmpeg", "ffprobe", "whisper-cli"}) ||
			len(capability.ResourceIDs) != 1 ||
			resources[capability.ResourceIDs[0]].Kind != ResourceKindTranscriptionConformanceModel ||
			!slices.Contains(capability.NoticeIDs, "whisper.cpp-license") {
			return fmt.Errorf("local transcription capability closure is invalid")
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
	case CapabilityLocalTranscriptionV1:
		return ConformanceLocalTranscriptionV1, true
	default:
		return "", false
	}
}

func strictlySortedUnique(values []string) bool {
	previous := ""
	for _, value := range values {
		if !identifier.MatchString(value) || value <= previous {
			return false
		}
		previous = value
	}
	return true
}

func resourceClosureDigest(record ResourceRecord) (string, error) {
	files := make([]mediaclosure.File, len(record.Files))
	for index, file := range record.Files {
		files[index] = mediaclosure.File{Path: file.Path, SHA256: file.SHA256, ByteSize: file.ByteSize}
	}
	return mediaclosure.ResourceDigest(mediaclosure.Resource{
		ID: record.ID, Kind: record.Kind, Version: record.Version, Root: record.Root, Files: files,
	})
}

func capabilityClosureDigest(
	record CapabilityRecord,
	tools map[string]ToolRecord,
	resources map[string]ResourceRecord,
	notices map[string]NoticeRecord,
) (string, error) {
	type item struct {
		ID       string `json:"id"`
		SHA256   string `json:"sha256"`
		ByteSize uint64 `json:"byteSize,omitempty"`
	}
	toolItems := make([]item, 0, len(record.ToolIDs))
	for _, id := range record.ToolIDs {
		value, exists := tools[id]
		if !exists {
			return "", fmt.Errorf("unknown tool %s", id)
		}
		toolItems = append(toolItems, item{ID: id, SHA256: value.SHA256, ByteSize: value.ByteSize})
	}
	resourceItems := make([]item, 0, len(record.ResourceIDs))
	for _, id := range record.ResourceIDs {
		value, exists := resources[id]
		if !exists {
			return "", fmt.Errorf("unknown resource %s", id)
		}
		resourceItems = append(resourceItems, item{ID: id, SHA256: value.SHA256})
	}
	noticeItems := make([]item, 0, len(record.NoticeIDs))
	for _, id := range record.NoticeIDs {
		value, exists := notices[id]
		if !exists {
			return "", fmt.Errorf("unknown notice %s", id)
		}
		noticeItems = append(noticeItems, item{ID: id, SHA256: value.SHA256, ByteSize: value.ByteSize})
	}
	return closureDigest("open-cut/media-capability-closure/v1", struct {
		ID                          string `json:"id"`
		EntryToolID                 string `json:"entryToolId"`
		ConformanceProfile          string `json:"conformanceProfile"`
		ConformanceSuiteSHA256      string `json:"conformanceSuiteSha256"`
		ConformanceEvidenceNoticeID string `json:"conformanceEvidenceNoticeId"`
		Tools                       []item `json:"tools"`
		Resources                   []item `json:"resources"`
		Notices                     []item `json:"notices"`
	}{
		ID: record.ID, EntryToolID: record.EntryToolID,
		ConformanceProfile: record.ConformanceProfile, ConformanceSuiteSHA256: record.ConformanceSuiteSHA256,
		ConformanceEvidenceNoticeID: record.ConformanceEvidenceNoticeID,
		Tools:                       toolItems, Resources: resourceItems, Notices: noticeItems,
	})
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
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte(domain))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(encoded)
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), nil
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
	result = append(result, whisperSourceRecord())
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
	if !validRelative(relative) {
		return "", fmt.Errorf("path is not a clean relative slash path")
	}
	candidate := filepath.Join(root, filepath.FromSlash(relative))
	physical, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	if filepath.Clean(physical) != filepath.Clean(candidate) {
		return "", fmt.Errorf("linked directories are forbidden")
	}
	contained, err := filepath.Rel(root, physical)
	if err != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(contained) {
		return "", fmt.Errorf("path escapes the API artifact root")
	}
	info, err := os.Lstat(physical)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("path is not a directory")
	}
	return physical, nil
}

func verifyResourceTree(root string, declared []ResourceFileRecord) error {
	want := make(map[string]struct{}, len(declared))
	for _, file := range declared {
		want[file.Path] = struct{}{}
	}
	seen := make(map[string]struct{}, len(declared))
	err := filepath.WalkDir(root, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filename == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("resource tree contains a linked entry")
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("resource tree contains a non-regular entry")
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if _, exists := want[relative]; !exists {
			return fmt.Errorf("resource tree contains undeclared file %s", relative)
		}
		seen[relative] = struct{}{}
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) != len(want) {
		return fmt.Errorf("resource tree is missing a declared file")
	}
	return nil
}

func resolveContainedRegular(root, relative string) (string, error) {
	if !validRelative(relative) {
		return "", fmt.Errorf("path is not a clean relative slash path")
	}
	candidate := filepath.Join(root, filepath.FromSlash(relative))
	physical, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	if filepath.Clean(physical) != filepath.Clean(candidate) {
		return "", fmt.Errorf("linked files are forbidden")
	}
	contained, err := filepath.Rel(root, physical)
	if err != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) || filepath.IsAbs(contained) {
		return "", fmt.Errorf("path escapes the API artifact root")
	}
	info, err := os.Lstat(physical)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("path is not a regular file")
	}
	return physical, nil
}

func digestFile(filename string) (string, uint64, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	digest := sha256.New()
	size, err := io.Copy(digest, file)
	if err != nil || size <= 0 {
		return "", 0, fmt.Errorf("digest media tool: %w", err)
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), uint64(size), nil
}

func validRelative(value string) bool {
	return value != "" && !strings.ContainsRune(value, '\\') && !path.IsAbs(value) &&
		path.Clean(value) == value && value != ".." && !strings.HasPrefix(value, "../")
}

func validDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(value[7:])
	return err == nil && value == strings.ToLower(value)
}

func cleanAbsolute(value string) bool {
	if value == "" || !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return false
	}
	if runtime.GOOS == "windows" {
		return !strings.ContainsRune(value, '\x00')
	}
	return true
}
