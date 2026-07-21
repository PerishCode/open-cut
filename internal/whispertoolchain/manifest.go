// Package whispertoolchain owns the local speech-transcription closure.
//
// It is deliberately a separate closure from the media toolchain because the
// two have opposite determinism contracts. The media toolchain's entire value
// is that its bytes never change: it is qualified by byte-exact repeated
// builds. Transcription is qualified semantically — the same machine must
// produce the same result twice — and it is free to use whatever backend the
// host offers. Welding the two together forced transcription to inherit a
// cross-machine byte contract it never needed, and left no place to say that a
// capability may legitimately differ per target.
//
// Consequences of the split, all intended:
//   - whisper.cpp's version no longer participates in the media toolchain's
//     identity, so bumping it cannot invalidate FFmpeg/libvpx/opus closures,
//     the shared C build tree or the media CI caches.
//   - the closure carries no FFmpeg. Audio reaches whisper already normalized
//     to canonical 16 kHz mono S16 by the API, and qualification uses a
//     synthesized fixture rather than a decoded one.
//   - a capability's conformance suite identity includes the build target, so
//     two targets running different backends can never share suite identity.
package whispertoolchain

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
	ManifestSchema = 1
	ManifestName   = "whisper-tools.json"
	ToolchainID    = "whisper"

	// LicenseProfile is whisper.cpp and ggml, both MIT. The closure carries no
	// LGPL component, which is precisely why it does not belong in the media
	// license profile.
	LicenseProfile = "mit-v1"

	ResourceKindConformanceModel = "transcription-conformance-model"

	CapabilityLocalTranscriptionV1  = "local-transcription-v1"
	ConformanceLocalTranscriptionV1 = "local-transcription-v1"

	ToolWhisperCLI = "whisper-cli"

	// capabilityClosureDomain keeps whisper capability identities distinct from
	// every other toolchain closure, so a same-named capability of another
	// closure can never be mistaken for this one.
	capabilityClosureDomain = "open-cut/whisper-capability-closure/v1"

	maximumManifestBytes = 64 << 10
)

var (
	ErrUnavailable = errors.New("whisper toolchain is unavailable")
	identifier     = toolchainclosure.Identifier
)

type (
	SourceRecord       = toolchainclosure.SourceRecord
	ToolRecord         = toolchainclosure.ToolRecord
	ResourceRecord     = toolchainclosure.ResourceRecord
	ResourceFileRecord = toolchainclosure.ResourceFileRecord
	CapabilityRecord   = toolchainclosure.CapabilityRecord
	NoticeRecord       = toolchainclosure.NoticeRecord
	Tool               = toolchainclosure.Tool
	Resource           = toolchainclosure.Resource
	ResourceFile       = toolchainclosure.ResourceFile
	Capability         = toolchainclosure.Capability
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

// BuildRecord carries only what this closure is built from. Backend names the
// acceleration backend the capability was qualified against; it is part of the
// recorded build identity because two targets may legitimately differ here.
type BuildRecord struct {
	RecipeSHA256  string   `json:"recipeSha256"`
	Compiler      string   `json:"compiler"`
	Backend       string   `json:"backend"`
	Configuration []string `json:"configuration"`
}

type Verified struct {
	Manifest     Manifest
	Root         string
	Tools        map[string]Tool
	Resources    map[string]Resource
	Capabilities map[string]Capability
}

func LoadForExecutable(executable string, expected target.Target) (Verified, error) {
	if !toolchainclosure.CleanAbsolute(executable) {
		return Verified{}, fmt.Errorf("whisper toolchain executable root is invalid")
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
	contents, err := toolchainclosure.Verify(root, expected, "whisper toolchain", toolchainclosure.Declaration{
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
	if !toolchainclosure.CleanAbsolute(root) || expected.Validate() != nil {
		return Manifest{}, "", fmt.Errorf("whisper toolchain root or target is invalid")
	}
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil || !toolchainclosure.CleanAbsolute(physicalRoot) {
		return Manifest{}, "", fmt.Errorf("resolve whisper toolchain root")
	}
	root = physicalRoot
	manifestPath := filepath.Join(root, ManifestName)
	info, err := os.Lstat(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, "", ErrUnavailable
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() <= 0 || info.Size() > maximumManifestBytes {
		return Manifest{}, "", fmt.Errorf("whisper toolchain manifest is invalid")
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return Manifest{}, "", fmt.Errorf("read whisper toolchain manifest: %w", err)
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return Manifest{}, "", fmt.Errorf("decode whisper toolchain manifest")
	}
	if err := validateManifest(manifest, expected); err != nil {
		return Manifest{}, "", err
	}
	return manifest, root, nil
}

func validateManifest(manifest Manifest, expected target.Target) error {
	if manifest.Schema != ManifestSchema || manifest.Target != expected ||
		manifest.ToolchainID != ToolchainID || manifest.Version != toolchainVersion ||
		manifest.LicenseProfile != LicenseProfile ||
		len(manifest.Tools) != 1 || len(manifest.Sources) != 1 ||
		len(manifest.Resources) != 1 || len(manifest.Capabilities) != 1 ||
		len(manifest.Notices) == 0 || len(manifest.Notices) > 16 {
		return fmt.Errorf("whisper toolchain manifest identity is invalid")
	}
	if !slices.Equal(manifest.Sources, sourceRecords()) {
		return fmt.Errorf("whisper toolchain source record is invalid")
	}
	if !toolchainclosure.ValidDigest(manifest.Build.RecipeSHA256) ||
		strings.TrimSpace(manifest.Build.Compiler) == "" || len(manifest.Build.Compiler) > 4096 ||
		!validBackend(manifest.Build.Backend, manifest.Target) ||
		!validConfiguration(manifest.Build.Configuration, manifest.Build.Backend, manifest.Target) ||
		!validRecordedConfiguration(manifest.Build.Configuration) {
		return fmt.Errorf("whisper toolchain build record is invalid")
	}

	tools := make(map[string]ToolRecord, len(manifest.Tools))
	for _, tool := range manifest.Tools {
		if tool.ID != ToolWhisperCLI || !toolchainclosure.ValidRelative(tool.Path) ||
			!toolchainclosure.ValidDigest(tool.SHA256) || tool.ByteSize == 0 {
			return fmt.Errorf("whisper toolchain tool record is invalid")
		}
		tools[tool.ID] = tool
	}

	resources := make(map[string]ResourceRecord, len(manifest.Resources))
	for _, resource := range manifest.Resources {
		if !identifier.MatchString(resource.ID) || resource.Kind != ResourceKindConformanceModel ||
			strings.TrimSpace(resource.Version) != resource.Version || resource.Version == "" ||
			len(resource.Version) > 128 || !toolchainclosure.ValidRelative(resource.Root) ||
			!toolchainclosure.ValidDigest(resource.SHA256) ||
			len(resource.Files) == 0 || len(resource.Files) > 16 {
			return fmt.Errorf("whisper toolchain resource record is invalid")
		}
		previous := ""
		for _, file := range resource.Files {
			if !toolchainclosure.ValidRelative(file.Path) || file.Path <= previous ||
				!toolchainclosure.ValidDigest(file.SHA256) || file.ByteSize == 0 {
				return fmt.Errorf("whisper toolchain resource file record is invalid")
			}
			previous = file.Path
		}
		digest, err := toolchainclosure.ResourceClosureDigest(resource)
		if err != nil || digest != resource.SHA256 {
			return fmt.Errorf("whisper toolchain resource closure digest is invalid")
		}
		resources[resource.ID] = resource
	}

	notices := make(map[string]NoticeRecord, len(manifest.Notices))
	for _, notice := range manifest.Notices {
		if !identifier.MatchString(notice.ID) || !toolchainclosure.ValidRelative(notice.Path) ||
			!toolchainclosure.ValidDigest(notice.SHA256) || notice.ByteSize == 0 {
			return fmt.Errorf("whisper toolchain notice record is invalid")
		}
		if _, duplicate := notices[notice.ID]; duplicate {
			return fmt.Errorf("whisper toolchain repeats a notice identity")
		}
		notices[notice.ID] = notice
	}

	for _, capability := range manifest.Capabilities {
		if capability.ID != CapabilityLocalTranscriptionV1 {
			return fmt.Errorf("whisper toolchain capability is unsupported")
		}
		if err := validateCapabilityShape(capability, resources); err != nil {
			return err
		}
		if !toolchainclosure.StrictlySortedUnique(capability.ToolIDs) ||
			!toolchainclosure.StrictlySortedUnique(capability.ResourceIDs) ||
			!toolchainclosure.StrictlySortedUnique(capability.NoticeIDs) {
			return fmt.Errorf("whisper toolchain capability identity list is invalid")
		}
		if capability.ConformanceProfile != ConformanceLocalTranscriptionV1 ||
			capability.ConformanceSuiteSHA256 != conformanceSuiteDigest(capability.ID, manifest.Target) ||
			capability.ConformanceEvidenceNoticeID != conformanceEvidenceNoticeID(capability.ID) {
			return fmt.Errorf("whisper toolchain capability conformance binding is invalid")
		}
		if _, exists := notices[capability.ConformanceEvidenceNoticeID]; !exists {
			return fmt.Errorf("whisper toolchain conformance evidence notice is unavailable")
		}
		digest, err := toolchainclosure.CapabilityClosureDigest(
			capabilityClosureDomain, capability, tools, resources, notices,
		)
		if err != nil || digest != capability.ClosureSHA256 {
			return fmt.Errorf("whisper toolchain capability closure digest is invalid")
		}
	}
	return nil
}

// validateCapabilityShape fixes the closure a transcription capability may
// reach. It carries whisper-cli and one conformance model — and notably no
// FFmpeg, because the API normalizes audio before whisper is ever invoked.
func validateCapabilityShape(capability CapabilityRecord, resources map[string]ResourceRecord) error {
	if capability.EntryToolID != ToolWhisperCLI ||
		!slices.Equal(capability.ToolIDs, []string{ToolWhisperCLI}) ||
		len(capability.ResourceIDs) != 1 ||
		resources[capability.ResourceIDs[0]].Kind != ResourceKindConformanceModel ||
		!slices.Contains(capability.NoticeIDs, WhisperLicenseNoticeID) {
		return fmt.Errorf("local transcription capability closure is invalid")
	}
	return nil
}

func bindManifestClosureDigests(manifest *Manifest) error {
	if manifest == nil {
		return fmt.Errorf("whisper manifest is required")
	}
	for index := range manifest.Resources {
		digest, err := toolchainclosure.ResourceClosureDigest(manifest.Resources[index])
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
		digest, err := toolchainclosure.CapabilityClosureDigest(
			capabilityClosureDomain, manifest.Capabilities[index], tools, resources, notices,
		)
		if err != nil {
			return err
		}
		manifest.Capabilities[index].ClosureSHA256 = digest
	}
	return nil
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
