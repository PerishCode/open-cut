package toolchainclosure

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
// GoSourceClosureFingerprint digests the exact Go package closure a build
// depends on, asked of the compiler rather than kept by hand.
//
// Domain separates one closure's fingerprint from another's. Tags and
// packagePath name what to resolve; passing the build tags the real build uses
// is not required, because listing a closure only needs imports to resolve.
func GoSourceClosureFingerprint(
	ctx context.Context, repositoryRoot, domain, tags, packagePath string,
) (string, error) {
	entries, err := FingerprintInputs(ctx, repositoryRoot, tags, packagePath)
	if err != nil {
		return "", err
	}
	goTool, err := tool.Resolve("go")
	if err != nil {
		return "", err
	}
	goVersion, err := goToolVersion(ctx, goTool)
	if err != nil {
		return "", err
	}
	overall := sha256.New()
	overall.Write([]byte(domain + "\n"))
	overall.Write([]byte(goVersion + "\n"))
	overall.Write([]byte(strings.Join(entries, "\n")))
	return "sha256:" + hex.EncodeToString(overall.Sum(nil)), nil
}

// FingerprintInputs returns the sorted, fully qualified input set the
// fingerprint digests: one entry per compiled first-party file and one per
// external module identity.
func FingerprintInputs(ctx context.Context, repositoryRoot, tags, packagePath string) ([]string, error) {
	packages, err := fingerprintPackages(ctx, repositoryRoot, tags, packagePath)
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
			return nil, fmt.Errorf("Go source closure package %s has no directory", current.ImportPath)
		}
		for _, name := range fingerprintFileNames(current) {
			path := filepath.Join(current.Dir, name)
			relativePath, err := filepath.Rel(repositoryRoot, path)
			if err != nil {
				return nil, err
			}
			digest, err := fingerprintFileDigest(path)
			if err != nil {
				return nil, fmt.Errorf("fingerprint Go source %s: %w", relativePath, err)
			}
			entries = append(entries, "file\x00"+filepath.ToSlash(relativePath)+"\x00"+digest)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("Go source closure is empty")
	}
	sort.Strings(entries)
	return entries, nil
}

// fingerprintPackages asks the Go toolchain for the renderer's exact
// package closure. It deliberately does not pass the native cgo include and
// link flags the real build uses: listing the closure only needs to resolve
// imports, so the fingerprint stays computable before the native dependencies
// have been built.
func fingerprintPackages(ctx context.Context, repositoryRoot, tags, packagePath string) ([]goPackage, error) {
	goTool, err := tool.Resolve("go")
	if err != nil {
		return nil, err
	}
	var output boundedBuffer
	output.Limit = maximumListBytes
	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: goTool,
		Args: []string{
			"list", "-deps", "-json", "-mod=readonly", "-tags", tags, packagePath,
		},
		Directory: repositoryRoot,
		Stdout:    &output, Stderr: io.Discard, Profile: lifecycle.ProfileDevelopment,
		Presentation: lifecycle.PresentationHeadless,
	}); err != nil || output.Exceeded {
		return nil, fmt.Errorf("inspect Go source closure: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	packages := make([]goPackage, 0, 256)
	for {
		var current goPackage
		err := decoder.Decode(&current)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode Go source closure: %w", err)
		}
		packages = append(packages, current)
	}
	return packages, nil
}

// fingerprintFileNames lists every file the toolchain reads for a
// first-party package under the renderer build tag, including the cgo and
// assembly inputs a Go-only walk would miss.
func fingerprintFileNames(current goPackage) []string {
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

// SplitFingerprintEntry decomposes one recorded input so a mismatch can be
// reported as "which input changed" rather than "the digest differs".
func SplitFingerprintEntry(entry string) (string, string, string) {
	parts := strings.Split(entry, "\x00")
	if len(parts) < 3 {
		return "malformed", entry, ""
	}
	return parts[0], parts[1], strings.Join(parts[2:], "\x00")
}
