package release

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/utils/target"
)

const ManifestSchema = 1

type Entry struct {
	Entry string `json:"entry"`
}

type Manifest struct {
	Schema                   int             `json:"schema"`
	Channel                  string          `json:"channel"`
	Version                  string          `json:"version"`
	Platform                 target.Platform `json:"platform"`
	Arch                     target.Arch     `json:"arch"`
	Launcher                 Entry           `json:"launcher"`
	Payload                  Entry           `json:"payload"`
	MinimumBootstrapProtocol string          `json:"minimumBootstrapProtocol"`
	PublishedAt              time.Time       `json:"publishedAt"`
}

func LoadManifest(filename string) (Manifest, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return Manifest{}, fmt.Errorf("read release manifest: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode release manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (manifest Manifest) Validate() error {
	if manifest.Schema != ManifestSchema {
		return fmt.Errorf("unsupported release manifest schema %d", manifest.Schema)
	}
	if _, err := ParseVersionForChannel(manifest.Version, manifest.Channel); err != nil {
		return err
	}
	if manifest.Platform == "" || manifest.Arch == "" || manifest.MinimumBootstrapProtocol == "" || manifest.PublishedAt.IsZero() {
		return fmt.Errorf("release manifest platform, arch, protocol, and publishedAt are required")
	}
	if err := (target.Target{Platform: manifest.Platform, Arch: manifest.Arch}).Validate(); err != nil {
		return fmt.Errorf("invalid release target: %w", err)
	}
	if _, err := validateEntry(manifest.Launcher.Entry, "launcher"); err != nil {
		return err
	}
	if _, err := validateEntry(manifest.Payload.Entry, "payload"); err != nil {
		return err
	}
	return nil
}

func (manifest Manifest) ValidateHost(channel, protocolFloor string) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	if manifest.Channel != channel {
		return fmt.Errorf("manifest channel %q does not match cell channel %q", manifest.Channel, channel)
	}
	host := target.Host()
	if manifest.Platform != host.Platform || manifest.Arch != host.Arch {
		return fmt.Errorf("manifest target %s-%s does not match host %s", manifest.Platform, manifest.Arch, host)
	}
	if manifest.MinimumBootstrapProtocol != protocolFloor {
		return fmt.Errorf("manifest requires bootstrap protocol %q, host provides %q", manifest.MinimumBootstrapProtocol, protocolFloor)
	}
	return nil
}

func ResolveEntry(versionRoot, entry, kind string) (string, error) {
	clean, err := validateEntry(entry, kind)
	if err != nil {
		return "", err
	}
	resolved := filepath.Join(versionRoot, filepath.FromSlash(clean))
	rootWithSeparator := filepath.Clean(versionRoot) + string(filepath.Separator)
	if !strings.HasPrefix(resolved, rootWithSeparator) {
		return "", fmt.Errorf("%s entry escapes version root", kind)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat %s entry: %w", kind, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s entry is not a regular file", kind)
	}
	return resolved, nil
}

func validateEntry(entry, kind string) (string, error) {
	if entry == "" || strings.ContainsRune(entry, '\\') || path.IsAbs(entry) {
		return "", fmt.Errorf("invalid %s entry %q", kind, entry)
	}
	clean := path.Clean(entry)
	if clean != entry || !strings.HasPrefix(clean, kind+"/") || clean == kind+"/" {
		return "", fmt.Errorf("%s entry must be a clean path beneath %s/", kind, kind)
	}
	return clean, nil
}
