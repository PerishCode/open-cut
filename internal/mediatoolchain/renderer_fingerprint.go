package mediatoolchain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/PerishCode/open-cut/internal/toolchainclosure"
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
	return toolchainclosure.GoSourceClosureFingerprint(
		ctx, repositoryRoot, "renderer-source-closure-v2", RendererBuildTag, RendererBuildPackage,
	)
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
	entries, err := toolchainclosure.FingerprintInputs(ctx, repositoryRoot, RendererBuildTag, RendererBuildPackage)
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
	current, err := toolchainclosure.FingerprintInputs(ctx, repositoryRoot, RendererBuildTag, RendererBuildPackage)
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
