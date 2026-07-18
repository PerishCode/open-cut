package mediatoolchain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// rendererSourceFingerprintName holds a dev-side digest of the renderer's
// first-party source next to the built closure. It is not part of the signed
// media manifest or its conformance evidence; it exists solely so the media
// build's reuse fast path can notice that the renderer's own source changed
// and rebuild, instead of silently shipping a stale open-cut-render.
const rendererSourceFingerprintName = "renderer-source.fingerprint"

// rendererSourceRoots are the first-party trees compiled into open-cut-render.
// Native-text C inputs are pinned source archives keyed by the recipe digest,
// so they cannot drift under a working tree; only these Go trees can.
var rendererSourceRoots = []string{
	filepath.Join("internal", "renderengine"),
	filepath.Join("internal", "renderhelper"),
	filepath.Join("internal", "rendernative"),
	filepath.Join("cmd", "open-cut-render"),
}

// RendererSourceFingerprint hashes every file under the renderer source trees
// (path and content, tests included) into one stable digest. Any edit to the
// renderer's source changes it.
func RendererSourceFingerprint(repositoryRoot string) (string, error) {
	entries := make([]string, 0, 256)
	for _, relative := range rendererSourceRoots {
		root := filepath.Join(repositoryRoot, relative)
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !entry.Type().IsRegular() {
				return nil
			}
			relativePath, err := filepath.Rel(repositoryRoot, path)
			if err != nil {
				return err
			}
			digest, err := fingerprintFileDigest(path)
			if err != nil {
				return err
			}
			entries = append(entries, filepath.ToSlash(relativePath)+"\x00"+digest)
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("fingerprint renderer source %s: %w", relative, err)
		}
	}
	sort.Strings(entries)
	overall := sha256.New()
	overall.Write([]byte(strings.Join(entries, "\n")))
	return "sha256:" + hex.EncodeToString(overall.Sum(nil)), nil
}

func fingerprintFileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func rendererSourceFingerprintMatches(repositoryRoot, artifactRoot string) bool {
	recorded, err := os.ReadFile(filepath.Join(artifactRoot, rendererSourceFingerprintName))
	if err != nil {
		return false
	}
	current, err := RendererSourceFingerprint(repositoryRoot)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(recorded)) == current
}

func writeRendererSourceFingerprint(repositoryRoot, artifactRoot string) error {
	current, err := RendererSourceFingerprint(repositoryRoot)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(artifactRoot, rendererSourceFingerprintName), []byte(current+"\n"), 0o644)
}
