package mediatoolchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/PerishCode/open-cut/internal/toolchainclosure"
	"github.com/PerishCode/open-cut/utils/tool"
)

// rendererSourceFingerprintName holds a dev-side digest of the renderer's
// source closure next to the built closure. It is not part of the signed media
// manifest or its conformance evidence; it exists solely so the media build's
// reuse fast path can notice that the renderer's source changed and rebuild,
// instead of silently shipping a stale open-cut-render.
const rendererSourceFingerprintName = "renderer-source.fingerprint"

// rendererSourceInputsName records the exact input set behind the digest, so a
// mismatch can be explained instead of merely reported.
const rendererSourceInputsName = "renderer-source.inputs"

// RendererSourceFingerprint keeps its own domain so the renderer's closure
// identity can never collide with another group's, even if both ever resolved
// to the same package set.
func RendererSourceFingerprint(ctx context.Context, repositoryRoot string) (string, error) {
	entries, err := rendererSourceFingerprintInputs(ctx, repositoryRoot)
	if err != nil {
		return "", err
	}
	goTool, err := tool.Resolve("go")
	if err != nil {
		return "", err
	}
	goVersion, err := toolchainclosure.GoToolVersion(ctx, goTool)
	if err != nil {
		return "", err
	}
	overall := sha256.New()
	overall.Write([]byte("renderer-source-closure-v2\n"))
	overall.Write([]byte(goVersion + "\n"))
	overall.Write([]byte(strings.Join(entries, "\n")))
	return "sha256:" + hex.EncodeToString(overall.Sum(nil)), nil
}

func rendererSourceFingerprintMatches(ctx context.Context, repositoryRoot, artifactRoot string) bool {
	recorded, err := os.ReadFile(filepath.Join(artifactRoot, rendererSourceFingerprintName))
	if err != nil {
		return false
	}
	current, err := RendererSourceFingerprint(ctx, repositoryRoot)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(recorded)) == current
}

func writeRendererSourceFingerprint(ctx context.Context, repositoryRoot, artifactRoot string) error {
	current, err := RendererSourceFingerprint(ctx, repositoryRoot)
	if err != nil {
		return err
	}
	if err := os.WriteFile(
		filepath.Join(artifactRoot, rendererSourceFingerprintName), []byte(current+"\n"), 0o644,
	); err != nil {
		return err
	}
	// The inputs are recorded beside the digest so a mismatch can name what
	// actually changed. A digest alone can only say that something did, which
	// is not enough to tell a genuine source change from a fingerprint that is
	// unstable across environments.
	entries, err := rendererSourceFingerprintInputs(ctx, repositoryRoot)
	if err != nil {
		return err
	}
	return os.WriteFile(
		filepath.Join(artifactRoot, rendererSourceInputsName),
		[]byte(strings.Join(entries, "\n")+"\n"), 0o644,
	)
}

// explainRendererFingerprintMismatch names the inputs that differ between the
// recorded closure and the working tree, bounded so a wholesale difference
// cannot flood a build log.
func explainRendererFingerprintMismatch(ctx context.Context, repositoryRoot, artifactRoot string) string {
	recorded, err := os.ReadFile(filepath.Join(artifactRoot, rendererSourceInputsName))
	if err != nil {
		return "recorded inputs are unavailable"
	}
	current, err := rendererSourceFingerprintInputs(ctx, repositoryRoot)
	if err != nil {
		return "current inputs are uncomputable"
	}
	before := make(map[string]string, 256)
	for _, entry := range strings.Split(strings.TrimSpace(string(recorded)), "\n") {
		kind, name, value := toolchainclosure.SplitFingerprintEntry(entry)
		before[kind+" "+name] = value
	}
	differences := make([]string, 0, 8)
	for _, entry := range current {
		kind, name, value := toolchainclosure.SplitFingerprintEntry(entry)
		key := kind + " " + name
		previous, present := before[key]
		switch {
		case !present:
			differences = append(differences, "added "+key)
		case previous != value:
			differences = append(differences, "changed "+key)
		}
		delete(before, key)
	}
	for key := range before {
		differences = append(differences, "removed "+key)
	}
	if len(differences) == 0 {
		return "inputs are identical; the digest differs for another reason"
	}
	sort.Strings(differences)
	const maximumReported = 8
	if len(differences) > maximumReported {
		return fmt.Sprintf(
			"%d inputs differ, including %s", len(differences), strings.Join(differences[:maximumReported], ", "),
		)
	}
	return strings.Join(differences, ", ")
}

func rendererSourceFingerprintInputs(ctx context.Context, repositoryRoot string) ([]string, error) {
	packages, _, err := rendererSourceGraph(
		ctx, repositoryRoot, "", "", io.Discard, io.Discard,
	)
	if err != nil {
		return nil, err
	}
	entries := make([]string, 0, 256)
	modules := make(map[string]struct{})
	for _, current := range packages {
		if current.Module == nil {
			continue
		}
		if !current.Module.Main {
			modules["module\x00"+current.Module.Path+"\x00"+current.Module.Version+"\x00"+current.Module.Sum] =
				struct{}{}
			continue
		}
		for _, name := range rendererPackageFiles(current) {
			source := filepath.Join(current.Dir, name)
			relative, err := filepath.Rel(repositoryRoot, source)
			if err != nil {
				return nil, err
			}
			digest, err := rendererSourceFileDigest(source)
			if err != nil {
				return nil, fmt.Errorf("fingerprint renderer source %s: %w", relative, err)
			}
			entries = append(entries, "file\x00"+filepath.ToSlash(relative)+"\x00"+digest)
		}
	}
	for module := range modules {
		entries = append(entries, module)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("renderer source closure is empty")
	}
	sort.Strings(entries)
	return entries, nil
}

func rendererSourceFileDigest(path string) (string, error) {
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
