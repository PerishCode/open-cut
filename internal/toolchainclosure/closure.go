// Package toolchainclosure carries the mechanism shared by every source-built
// toolchain closure the API owns: contained-path resolution, exact byte
// verification, resource-tree closure and domain-separated closure digests.
//
// It deliberately owns no schema. Each toolchain declares its own manifest,
// its own identity gates and its own determinism contract, then calls this
// package to verify the bytes those declarations point at. That split is the
// point: a byte-reproducible codec toolchain and a semantically-stable
// inference toolchain share how bytes are proven, not what must be proven.
package toolchainclosure

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/PerishCode/open-cut/internal/mediaclosure"
	"github.com/PerishCode/open-cut/utils/target"
)

// Identifier is the shared identity shape for tools, resources, notices and
// capabilities across every toolchain closure.
var Identifier = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,63}$`)

type SourceRecord struct {
	ID           string `json:"id"`
	Version      string `json:"version"`
	URL          string `json:"url"`
	SignatureURL string `json:"signatureUrl,omitempty"`
	SHA256       string `json:"sha256"`
	License      string `json:"license"`
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

// Contents is the verified projection of one closure's declared bytes.
type Contents struct {
	Tools        map[string]Tool
	Resources    map[string]Resource
	Notices      map[string]NoticeRecord
	Capabilities map[string]Capability
}

// Declaration is the record set a manifest points at. The caller has already
// applied its own identity and shape gates; this package proves the bytes.
type Declaration struct {
	Tools        []ToolRecord
	Resources    []ResourceRecord
	Notices      []NoticeRecord
	Capabilities []CapabilityRecord
}

// Verify resolves every declared path under root, confirms exact digest and
// size, rejects links and undeclared resource-tree entries, and binds the
// capability projection. Label names the closure in diagnostics.
func Verify(
	root string, expected target.Target, label string, declaration Declaration,
) (Contents, error) {
	tools := make(map[string]Tool, len(declaration.Tools))
	for _, record := range declaration.Tools {
		filename, err := ResolveContainedRegular(root, record.Path)
		if err != nil {
			return Contents{}, fmt.Errorf("validate %s tool %s: %w", label, record.ID, err)
		}
		actualDigest, actualSize, err := DigestFile(filename)
		if err != nil || actualDigest != record.SHA256 || actualSize != record.ByteSize {
			return Contents{}, fmt.Errorf("validate %s tool %s: digest or size mismatch", label, record.ID)
		}
		if expected.Platform != target.Win {
			toolInfo, statErr := os.Stat(filename)
			if statErr != nil || toolInfo.Mode().Perm()&0o111 == 0 {
				return Contents{}, fmt.Errorf("validate %s tool %s: executable bit is unavailable", label, record.ID)
			}
		}
		tools[record.ID] = Tool{
			ID: record.ID, Path: filename, SHA256: record.SHA256, ByteSize: record.ByteSize,
		}
	}
	resources := make(map[string]Resource, len(declaration.Resources))
	for _, record := range declaration.Resources {
		resourceRoot, err := ResolveContainedDirectory(root, record.Root)
		if err != nil {
			return Contents{}, fmt.Errorf("validate %s resource %s: %w", label, record.ID, err)
		}
		files := make([]ResourceFile, 0, len(record.Files))
		for _, declared := range record.Files {
			relative := path.Join(record.Root, declared.Path)
			filename, err := ResolveContainedRegular(root, relative)
			if err != nil {
				return Contents{}, fmt.Errorf(
					"validate %s resource %s file %s: %w", label, record.ID, declared.Path, err,
				)
			}
			actualDigest, actualSize, err := DigestFile(filename)
			if err != nil || actualDigest != declared.SHA256 || actualSize != declared.ByteSize {
				return Contents{}, fmt.Errorf(
					"validate %s resource %s file %s: digest or size mismatch", label, record.ID, declared.Path,
				)
			}
			files = append(files, ResourceFile{
				Path: filename, SHA256: declared.SHA256, ByteSize: declared.ByteSize,
			})
		}
		if err := VerifyResourceTree(resourceRoot, record.Files); err != nil {
			return Contents{}, fmt.Errorf("validate %s resource %s: %w", label, record.ID, err)
		}
		resources[record.ID] = Resource{
			ID: record.ID, Kind: record.Kind, Version: record.Version, Root: resourceRoot,
			SHA256: record.SHA256, Files: files,
		}
	}
	notices := make(map[string]NoticeRecord, len(declaration.Notices))
	for _, notice := range declaration.Notices {
		filename, err := ResolveContainedRegular(root, notice.Path)
		if err != nil {
			return Contents{}, fmt.Errorf("validate %s notice %s: %w", label, notice.ID, err)
		}
		actualDigest, actualSize, err := DigestFile(filename)
		if err != nil || actualDigest != notice.SHA256 || actualSize != notice.ByteSize {
			return Contents{}, fmt.Errorf("validate %s notice %s: digest or size mismatch", label, notice.ID)
		}
		notices[notice.ID] = notice
	}
	capabilities := make(map[string]Capability, len(declaration.Capabilities))
	for _, record := range declaration.Capabilities {
		capability := Capability{
			ID:                     record.ID,
			Entry:                  tools[record.EntryToolID],
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
	return Contents{
		Tools: tools, Resources: resources, Notices: notices, Capabilities: capabilities,
	}, nil
}

// ResourceClosureDigest is the aggregate identity of one declared resource tree.
func ResourceClosureDigest(record ResourceRecord) (string, error) {
	files := make([]mediaclosure.File, len(record.Files))
	for index, file := range record.Files {
		files[index] = mediaclosure.File{
			Path: file.Path, SHA256: file.SHA256, ByteSize: file.ByteSize,
		}
	}
	return mediaclosure.ResourceDigest(mediaclosure.Resource{
		ID: record.ID, Kind: record.Kind, Version: record.Version, Root: record.Root, Files: files,
	})
}

// CapabilityClosureDigest binds a capability to the exact bytes it may reach.
// Domain separates one toolchain's capability identities from another's, so a
// capability can never be mistaken for a same-named capability of a different
// closure.
func CapabilityClosureDigest(
	domain string,
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
	return ClosureDigest(domain, struct {
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

// ClosureDigest is the domain-separated canonical digest every closure identity
// is built from.
func ClosureDigest(domain string, value any) (string, error) {
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

func ResolveContainedDirectory(root, relative string) (string, error) {
	if !ValidRelative(relative) {
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
		return "", fmt.Errorf("path escapes the artifact root")
	}
	info, err := os.Lstat(physical)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("path is not a directory")
	}
	return physical, nil
}

func VerifyResourceTree(root string, declared []ResourceFileRecord) error {
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

func ResolveContainedRegular(root, relative string) (string, error) {
	if !ValidRelative(relative) {
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
	if err != nil || contained == ".." ||
		strings.HasPrefix(contained, ".."+string(filepath.Separator)) || filepath.IsAbs(contained) {
		return "", fmt.Errorf("path escapes the artifact root")
	}
	info, err := os.Lstat(physical)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("path is not a regular file")
	}
	return physical, nil
}

func DigestFile(filename string) (string, uint64, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	digest := sha256.New()
	size, err := io.Copy(digest, file)
	if err != nil || size <= 0 {
		return "", 0, fmt.Errorf("digest closure file: %w", err)
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), uint64(size), nil
}

func ValidRelative(value string) bool {
	return value != "" && !strings.ContainsRune(value, '\\') && !path.IsAbs(value) &&
		path.Clean(value) == value && value != ".." && !strings.HasPrefix(value, "../")
}

func ValidDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(value[7:])
	return err == nil && value == strings.ToLower(value)
}

func CleanAbsolute(value string) bool {
	if value == "" || !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return false
	}
	if runtime.GOOS == "windows" {
		return !strings.ContainsRune(value, '\x00')
	}
	return true
}

// StrictlySortedUnique enforces the sorted, duplicate-free identity lists every
// capability record carries.
func StrictlySortedUnique(values []string) bool {
	previous := ""
	for _, value := range values {
		if !Identifier.MatchString(value) || value <= previous {
			return false
		}
		previous = value
	}
	return true
}
