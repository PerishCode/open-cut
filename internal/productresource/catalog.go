package productresource

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/utils/atomicfile"
)

const (
	CatalogSchema           = 1
	CatalogName             = "product-resources.json"
	CatalogID               = "open-cut-product-resources"
	TranscriptModelName     = "whisper-small-multilingual-v1"
	TranscriptModelVersion  = "whisper-small@c521a4b02f422512"
	TranscriptModelOrigin   = "https://huggingface.co/ggerganov/whisper.cpp/resolve/c521a4b02f422512d734391fdf08bb08c0862f68/ggml-small.bin?download=true"
	TranscriptModelByteSize = uint64(487_601_967)
	TranscriptModelSHA256   = "sha256:1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b"
	maximumCatalogBytes     = 256 << 10
)

var ErrUnavailable = errors.New("product resource catalog is unavailable")

func DefaultResources() []ResourceEntry {
	byteSize, _ := domain.NewUInt64(TranscriptModelByteSize)
	return []ResourceEntry{{
		Name: TranscriptModelName, Kind: domain.ProductResourceTranscriptionModel,
		Version: TranscriptModelVersion, Profile: TranscriptModelName, Origin: TranscriptModelOrigin,
		ByteSize: byteSize, SHA256: domain.Digest(TranscriptModelSHA256),
		Retention: domain.ProductResourceRetentionOffline,
	}}
}

type Manifest struct {
	Schema    int             `json:"schema"`
	CatalogID string          `json:"catalogId"`
	Version   string          `json:"version"`
	Resources []ResourceEntry `json:"resources"`
}

type ResourceEntry struct {
	Name      string                          `json:"name"`
	Kind      domain.ProductResourceKind      `json:"kind"`
	Version   string                          `json:"version"`
	Profile   string                          `json:"profile"`
	Origin    string                          `json:"origin"`
	ByteSize  domain.UInt64                   `json:"byteSize"`
	SHA256    domain.Digest                   `json:"sha256"`
	Retention domain.ProductResourceRetention `json:"retention"`
}

type Verified struct {
	Manifest Manifest
	Root     string
	Entries  []application.ProductResourceCatalogEntry
}

func Write(root, version string, resources []ResourceEntry) error {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root ||
		version == "" || len(version) > 128 || len(resources) > 128 {
		return fmt.Errorf("product resource catalog output is invalid")
	}
	manifest := Manifest{
		Schema: CatalogSchema, CatalogID: CatalogID, Version: version,
		Resources: append([]ResourceEntry(nil), resources...),
	}
	sort.Slice(manifest.Resources, func(left, right int) bool {
		return manifest.Resources[left].Name < manifest.Resources[right].Name
	})
	entries := make([]application.ProductResourceCatalogEntry, 0, len(manifest.Resources))
	for _, resource := range manifest.Resources {
		entry, err := application.NewProductResourceCatalogEntry(
			resource.Name, resource.Kind, resource.Version, resource.Profile, resource.Origin,
			resource.ByteSize, resource.SHA256, resource.Retention,
		)
		if err != nil {
			return err
		}
		entries = append(entries, entry)
	}
	if err := application.ValidateProductResourceCatalog(entries); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	return atomicfile.WriteJSON(filepath.Join(root, CatalogName), manifest, 0o644)
}

func LoadForExecutable(executable string) (Verified, error) {
	if executable == "" || !filepath.IsAbs(executable) || filepath.Clean(executable) != executable {
		return Verified{}, fmt.Errorf("product resource executable root is invalid")
	}
	physical, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return Verified{}, fmt.Errorf("resolve API executable: %w", err)
	}
	return Load(filepath.Dir(physical))
}

func Load(root string) (Verified, error) {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return Verified{}, fmt.Errorf("product resource catalog root is invalid")
	}
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return Verified{}, fmt.Errorf("resolve product resource catalog root: %w", err)
	}
	path := filepath.Join(physicalRoot, CatalogName)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Verified{}, ErrUnavailable
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() <= 0 || info.Size() > maximumCatalogBytes {
		return Verified{}, fmt.Errorf("product resource catalog is invalid")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Verified{}, err
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		manifest.Schema != CatalogSchema || manifest.CatalogID != CatalogID ||
		manifest.Version == "" || len(manifest.Version) > 128 || len(manifest.Resources) > 128 {
		return Verified{}, fmt.Errorf("decode product resource catalog")
	}
	entries := make([]application.ProductResourceCatalogEntry, 0, len(manifest.Resources))
	for _, resource := range manifest.Resources {
		entry, err := application.NewProductResourceCatalogEntry(
			resource.Name, resource.Kind, resource.Version, resource.Profile, resource.Origin,
			resource.ByteSize, resource.SHA256, resource.Retention,
		)
		if err != nil {
			return Verified{}, fmt.Errorf("validate product resource %s: %w", resource.Name, err)
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name < entries[right].Name })
	for index := 1; index < len(entries); index++ {
		if entries[index-1].Name == entries[index].Name {
			return Verified{}, fmt.Errorf("product resource catalog repeats %s", entries[index].Name)
		}
	}
	if err := application.ValidateProductResourceCatalog(entries); err != nil {
		return Verified{}, err
	}
	return Verified{Manifest: manifest, Root: physicalRoot, Entries: entries}, nil
}
