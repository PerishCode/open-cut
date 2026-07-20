package mediatoolchain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/PerishCode/open-cut/lifecycle"
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

// RendererSourceFingerprint digests everything the Go toolchain would compile
// into open-cut-render: the content of every first-party package in its
// dependency closure, the identity of every external module it links, and the
// toolchain that builds it.
//
// The closure is asked of `go list`, not maintained by hand. A hand-kept list
// of source trees is only ever an approximation of what the compiler actually
// reads, and the approximation fails silently: the helper keeps being reused
// while a dependency it does compile in has changed underneath it. That is the
// exact failure this fingerprint exists to prevent, so its input set has to
// come from the same authority the compiler uses.
//
// The build tag matters. open-cut-render compiles its native caption path
// under it, and the closure differs without it.
func RendererSourceFingerprint(ctx context.Context, repositoryRoot string) (string, error) {
	entries, err := rendererFingerprintInputs(ctx, repositoryRoot)
	if err != nil {
		return "", err
	}
	goTool, err := tool.Resolve("go")
	if err != nil {
		return "", err
	}
	goVersion, err := rendererGoVersion(ctx, goTool)
	if err != nil {
		return "", err
	}
	overall := sha256.New()
	overall.Write([]byte("renderer-source-closure-v2\n"))
	overall.Write([]byte(goVersion + "\n"))
	overall.Write([]byte(strings.Join(entries, "\n")))
	return "sha256:" + hex.EncodeToString(overall.Sum(nil)), nil
}

// rendererFingerprintInputs returns the sorted, fully qualified input set the
// fingerprint digests: one entry per compiled first-party file and one per
// external module identity.
func rendererFingerprintInputs(ctx context.Context, repositoryRoot string) ([]string, error) {
	packages, err := rendererFingerprintPackages(ctx, repositoryRoot)
	if err != nil {
		return nil, err
	}
	entries := make([]string, 0, 256)
	for _, current := range packages {
		if current.Standard || current.Module == nil {
			continue
		}
		if !current.Module.Main {
			// An external module contributes its exact pinned identity; its
			// bytes are already fixed by the module sum.
			entries = append(entries, "module\x00"+current.Module.Path+"\x00"+current.Module.Version+"\x00"+current.Module.Sum)
			continue
		}
		if current.Dir == "" {
			return nil, fmt.Errorf("renderer source closure package %s has no directory", current.ImportPath)
		}
		for _, name := range rendererFingerprintFileNames(current) {
			path := filepath.Join(current.Dir, name)
			relativePath, err := filepath.Rel(repositoryRoot, path)
			if err != nil {
				return nil, err
			}
			digest, err := fingerprintFileDigest(path)
			if err != nil {
				return nil, fmt.Errorf("fingerprint renderer source %s: %w", relativePath, err)
			}
			entries = append(entries, "file\x00"+filepath.ToSlash(relativePath)+"\x00"+digest)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("renderer source closure is empty")
	}
	sort.Strings(entries)
	return entries, nil
}

// rendererFingerprintPackages asks the Go toolchain for the renderer's exact
// package closure. It deliberately does not pass the native cgo include and
// link flags the real build uses: listing the closure only needs to resolve
// imports, so the fingerprint stays computable before the native dependencies
// have been built.
func rendererFingerprintPackages(ctx context.Context, repositoryRoot string) ([]rendererGoPackage, error) {
	goTool, err := tool.Resolve("go")
	if err != nil {
		return nil, err
	}
	var output rendererBoundedBuffer
	output.limit = maximumRendererListBytes
	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: goTool,
		Args: []string{
			"list", "-deps", "-json", "-mod=readonly", "-tags", RendererBuildTag, RendererBuildPackage,
		},
		Directory: repositoryRoot,
		Stdout:    &output, Stderr: io.Discard, Profile: lifecycle.ProfileDevelopment,
		Presentation: lifecycle.PresentationHeadless,
	}); err != nil || output.exceeded {
		return nil, fmt.Errorf("inspect renderer source closure: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	packages := make([]rendererGoPackage, 0, 256)
	for {
		var current rendererGoPackage
		err := decoder.Decode(&current)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode renderer source closure: %w", err)
		}
		packages = append(packages, current)
	}
	return packages, nil
}

// rendererFingerprintFileNames lists every file the toolchain reads for a
// first-party package under the renderer build tag, including the cgo and
// assembly inputs a Go-only walk would miss.
func rendererFingerprintFileNames(current rendererGoPackage) []string {
	names := make([]string, 0, 32)
	names = append(names, current.GoFiles...)
	names = append(names, current.CgoFiles...)
	names = append(names, current.CFiles...)
	names = append(names, current.CXXFiles...)
	names = append(names, current.HFiles...)
	names = append(names, current.SFiles...)
	names = append(names, current.EmbedFiles...)
	sort.Strings(names)
	return names
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
	entries, err := rendererFingerprintInputs(ctx, repositoryRoot)
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
	current, err := rendererFingerprintInputs(ctx, repositoryRoot)
	if err != nil {
		return "current inputs are uncomputable"
	}
	before := make(map[string]string, 256)
	for _, entry := range strings.Split(strings.TrimSpace(string(recorded)), "\n") {
		kind, name, value := splitFingerprintEntry(entry)
		before[kind+" "+name] = value
	}
	differences := make([]string, 0, 8)
	for _, entry := range current {
		kind, name, value := splitFingerprintEntry(entry)
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

func splitFingerprintEntry(entry string) (string, string, string) {
	parts := strings.Split(entry, "\x00")
	if len(parts) < 3 {
		return "malformed", entry, ""
	}
	return parts[0], parts[1], strings.Join(parts[2:], "\x00")
}
