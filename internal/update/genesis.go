package update

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/bundle"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/release"
	"github.com/PerishCode/open-cut/internal/runtimetopology"
	"github.com/PerishCode/open-cut/internal/state"
	"github.com/PerishCode/open-cut/utils/target"
)

type Installer struct {
	HTTPClient *http.Client
	Now        func() time.Time
}

func (installer Installer) InstallLatest(ctx context.Context, bootstrap config.Bootstrap, paths layout.CellPaths) (string, error) {
	if err := installer.Recover(bootstrap, paths); err != nil {
		return "", fmt.Errorf("recover interrupted update: %w", err)
	}
	client := installer.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	now := installer.Now
	if now == nil {
		now = time.Now
	}
	if len(bootstrap.UpdateOrigins) == 0 {
		return "", fmt.Errorf("no update origins configured")
	}
	if _, err := ensureTrustRoot(bootstrap, paths); err != nil {
		return "", fmt.Errorf("initialize trust root: %w", err)
	}
	var failures []error
	for _, origin := range bootstrap.UpdateOrigins {
		version, err := installer.installFromOrigin(ctx, client, now, origin, bootstrap, paths)
		if err == nil {
			return version, nil
		}
		failures = append(failures, fmt.Errorf("origin %s: %w", origin, err))
	}
	return "", errors.Join(failures...)
}

func (installer Installer) installFromOrigin(ctx context.Context, client *http.Client, now func() time.Time, origin string, bootstrap config.Bootstrap, paths layout.CellPaths) (string, error) {
	trustRoot, err := ensureTrustRoot(bootstrap, paths)
	if err != nil {
		return "", err
	}
	trustRoot, err = rotateTrustRoot(ctx, client, origin, trustRoot, paths)
	if err != nil {
		return "", fmt.Errorf("rotate trust root: %w", err)
	}
	host := target.Host()
	metadataURL, err := resolveMetadataURL(origin, bootstrap.Channel, host)
	if err != nil {
		return "", err
	}
	var envelope release.Envelope
	if err := fetchJSON(ctx, client, metadataURL, 1<<20, &envelope); err != nil {
		return "", err
	}
	if err := release.VerifyEnvelope(envelope, trustRoot.Config(), bootstrap.Channel, bootstrap.ProtocolFloor, now()); err != nil {
		return "", fmt.Errorf("verify release metadata: %w", err)
	}
	descriptor := envelope.Signed
	runtimeState, err := state.Load(paths.StateFile, bootstrap.Channel)
	if err != nil {
		return "", err
	}
	if runtimeState.Candidate != "" {
		if runtimeState.Candidate == descriptor.Version {
			return descriptor.Version, nil
		}
		return "", fmt.Errorf("candidate %s is already pending", runtimeState.Candidate)
	}
	if runtimeState.Active == descriptor.Version {
		return descriptor.Version, nil
	}
	if runtimeState.Active != "" {
		activeVersion, _ := release.ParseVersionForChannel(runtimeState.Active, bootstrap.Channel)
		candidateVersion, _ := release.ParseVersionForChannel(descriptor.Version, bootstrap.Channel)
		if candidateVersion.Compare(activeVersion) <= 0 {
			return "", fmt.Errorf("release rollback from %s to %s rejected", runtimeState.Active, descriptor.Version)
		}
	}
	transactionID, err := randomID()
	if err != nil {
		return "", err
	}
	transactionRoot := filepath.Join(paths.Incoming, transactionID)
	tree := filepath.Join(transactionRoot, "tree")
	entry := journal{
		TransactionID: transactionID, Channel: bootstrap.Channel, Version: descriptor.Version,
		SHA256: descriptor.Bundle.SHA256, Phase: "metadata-verified",
	}
	if err := saveJournal(paths.UpdateJournal, entry); err != nil {
		return "", err
	}
	succeeded := false
	defer func() {
		if !succeeded {
			_ = os.RemoveAll(transactionRoot)
			_ = os.Remove(paths.UpdateJournal)
		}
	}()
	cachePath := filepath.Join(paths.Downloads, descriptor.Bundle.SHA256+".tar.zst")
	bundleURL, err := resolveOriginPath(origin, descriptor.Bundle.Path)
	if err != nil {
		return "", err
	}
	if err := ensureBundle(ctx, client, descriptor.Bundle, bundleURL, cachePath); err != nil {
		return "", err
	}
	entry.Phase = "downloaded"
	if err := saveJournal(paths.UpdateJournal, entry); err != nil {
		return "", err
	}
	if err := bundle.Extract(cachePath, tree); err != nil {
		return "", fmt.Errorf("extract release bundle: %w", err)
	}
	entry.Phase = "extracted"
	if err := saveJournal(paths.UpdateJournal, entry); err != nil {
		return "", err
	}
	manifest, err := release.LoadManifest(filepath.Join(tree, "manifest.json"))
	if err != nil {
		return "", err
	}
	if manifest.Version != descriptor.Version || manifest.Channel != descriptor.Channel || manifest.Platform != descriptor.Platform || manifest.Arch != descriptor.Arch {
		return "", fmt.Errorf("inner manifest does not match signed release metadata")
	}
	if err := manifest.ValidateHost(bootstrap.Channel, bootstrap.ProtocolFloor); err != nil {
		return "", err
	}
	if _, err := release.ResolveEntry(tree, manifest.Launcher.Entry, "launcher"); err != nil {
		return "", err
	}
	topologyEntry, err := release.ResolveEntry(tree, manifest.Payload.Entry, "payload")
	if err != nil {
		return "", err
	}
	if _, err := runtimetopology.Resolve(topologyEntry); err != nil {
		return "", err
	}
	destination := filepath.Join(paths.Versions, descriptor.Version)
	if _, err := os.Stat(destination); errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(tree, destination); err != nil {
			return "", fmt.Errorf("promote release version: %w", err)
		}
	} else if err == nil {
		existing, loadErr := release.LoadManifest(filepath.Join(destination, "manifest.json"))
		if loadErr != nil || existing.Version != descriptor.Version {
			return "", fmt.Errorf("version %s already exists with invalid manifest", descriptor.Version)
		}
	} else if err != nil {
		return "", err
	}
	entry.Phase = "promoted"
	if err := saveJournal(paths.UpdateJournal, entry); err != nil {
		return "", err
	}
	prepared, err := state.Prepare(runtimeState, bootstrap.Channel, descriptor.Version)
	if err != nil {
		return "", err
	}
	if err := state.Save(paths.StateFile, bootstrap.Channel, prepared); err != nil {
		return "", err
	}
	if err := finishRecovery(paths.UpdateJournal, transactionRoot); err != nil {
		return "", err
	}
	succeeded = true
	return descriptor.Version, nil
}

func resolveMetadataURL(origin, channel string, target target.Target) (string, error) {
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("update origin must be absolute HTTP(S)")
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/metadata/" + url.PathEscape(channel) + "/" + target.String() + "/latest.json"
	parsed.RawQuery, parsed.Fragment = "", ""
	return parsed.String(), nil
}

func resolveOriginPath(origin, relative string) (string, error) {
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("update origin must be absolute HTTP(S)")
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/" + relative
	parsed.RawQuery, parsed.Fragment = "", ""
	return parsed.String(), nil
}

func fetchJSON(ctx context.Context, client *http.Client, source string, maxBytes int64, output any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s returned %s", source, response.Status)
	}
	limited := &io.LimitedReader{R: response.Body, N: maxBytes + 1}
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode %s: %w", source, err)
	}
	if limited.N <= 0 {
		return fmt.Errorf("metadata from %s exceeds %d bytes", source, maxBytes)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("metadata from %s contains trailing data", source)
	}
	return nil
}

func ensureBundle(ctx context.Context, client *http.Client, descriptor release.BundleDescriptor, sourceURL, destination string) error {
	if digest, size, err := bundle.SHA256(destination); err == nil && digest == descriptor.SHA256 && size == descriptor.Size {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	partial := destination + ".part"
	_ = os.Remove(partial)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download bundle returned %s", response.Status)
	}
	output, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, io.LimitReader(response.Body, descriptor.Size+1))
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil {
		os.Remove(partial)
		return copyErr
	}
	if syncErr != nil || closeErr != nil {
		os.Remove(partial)
		return errors.Join(syncErr, closeErr)
	}
	digest, size, err := bundle.SHA256(partial)
	if err != nil || digest != descriptor.SHA256 || size != descriptor.Size {
		os.Remove(partial)
		return fmt.Errorf("downloaded bundle digest or size mismatch")
	}
	if err := os.Rename(partial, destination); err != nil {
		os.Remove(partial)
		return err
	}
	return nil
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
