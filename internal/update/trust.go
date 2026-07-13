package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/PerishCode/open-cut/internal/atomicfile"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/release"
)

func ensureTrustRoot(bootstrap config.Bootstrap, paths layout.CellPaths) (release.TrustRoot, error) {
	data, err := os.ReadFile(paths.TrustRootFile)
	if errors.Is(err, os.ErrNotExist) {
		initial, initialErr := release.InitialTrustRoot(bootstrap.InitialTrustRoot)
		if initialErr != nil {
			return release.TrustRoot{}, initialErr
		}
		if writeErr := atomicfile.WriteJSON(paths.TrustRootFile, initial, 0o600); writeErr != nil {
			return release.TrustRoot{}, writeErr
		}
		return initial, nil
	}
	if err != nil {
		return release.TrustRoot{}, err
	}
	var root release.TrustRoot
	if err := json.Unmarshal(data, &root); err != nil {
		return release.TrustRoot{}, fmt.Errorf("decode persisted trust root: %w", err)
	}
	if err := root.Validate(); err != nil {
		return release.TrustRoot{}, fmt.Errorf("persisted trust root: %w", err)
	}
	return root, nil
}

func rotateTrustRoot(ctx context.Context, client *http.Client, origin string, current release.TrustRoot, paths layout.CellPaths) (release.TrustRoot, error) {
	rootURL, err := resolveRootURL(origin)
	if err != nil {
		return release.TrustRoot{}, err
	}
	var envelope release.TrustRootEnvelope
	found, err := fetchOptionalJSON(ctx, client, rootURL, 1<<20, &envelope)
	if err != nil {
		return release.TrustRoot{}, err
	}
	if !found {
		return current, nil
	}
	if envelope.Signed.Version == current.Version {
		return current, nil
	}
	if envelope.Signed.Version < current.Version {
		return release.TrustRoot{}, fmt.Errorf("trust root rollback from %d to %d rejected", current.Version, envelope.Signed.Version)
	}
	if err := release.VerifyTrustRoot(envelope, current); err != nil {
		return release.TrustRoot{}, err
	}
	if err := atomicfile.WriteJSON(paths.TrustRootFile, envelope.Signed, 0o600); err != nil {
		return release.TrustRoot{}, err
	}
	return envelope.Signed, nil
}

func resolveRootURL(origin string) (string, error) {
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("update origin must be absolute HTTP(S)")
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/metadata/root.json"
	parsed.RawQuery, parsed.Fragment = "", ""
	return parsed.String(), nil
}

func fetchOptionalJSON(ctx context.Context, client *http.Client, source string, maxBytes int64, output any) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return false, err
	}
	response, err := client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("GET %s returned %s", source, response.Status)
	}
	limited := &io.LimitedReader{R: response.Body, N: maxBytes + 1}
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(output); err != nil {
		return false, fmt.Errorf("decode %s: %w", source, err)
	}
	if limited.N <= 0 {
		return false, fmt.Errorf("metadata from %s exceeds %d bytes", source, maxBytes)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return false, fmt.Errorf("metadata from %s contains trailing data", source)
	}
	return true, nil
}
