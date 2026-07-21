package mediatoolchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"

	"github.com/PerishCode/open-cut/internal/toolchainclosure"
	"github.com/PerishCode/open-cut/utils/target"
	"github.com/PerishCode/open-cut/utils/tool"
)

// CacheKeys names the two reuse scopes a build environment can restore
// independently. They are separate because their inputs are: which archives to
// fetch is decided by the pinned catalog alone, while the built closure also
// embeds the renderer's compiled source and the toolchain that produced it.
// One shared key would re-download the pinned fonts - most of half a gigabyte,
// and stable for months - for every renderer edit.
type CacheKeys struct {
	Schema        int    `json:"schema"`
	Target        string `json:"target"`
	SourcePrefix  string `json:"sourcePrefix"`
	SourceKey     string `json:"sourceKey"`
	CBuildPrefix  string `json:"cbuildPrefix"`
	CBuildKey     string `json:"cbuildKey"`
	ClosurePrefix string `json:"closurePrefix"`
	ClosureKey    string `json:"closureKey"`
}

// CacheKeyOptions carries the environment facts a repository checkout cannot
// know. Environment identifies the host image the C toolchain is compiled
// against; a build environment that upgrades its compilers produces different
// bytes from identical sources, so it belongs in the closure key.
type CacheKeyOptions struct {
	RepositoryRoot string
	Target         target.Target
	Environment    string
}

// ComputeCacheKeys derives both keys from the same authorities the build
// itself uses, so a cache key cannot drift away from what it is supposed to
// describe. The renderer's contribution is its real dependency closure, asked
// of the Go toolchain rather than approximated by a list of directories.
func ComputeCacheKeys(ctx context.Context, options CacheKeyOptions) (CacheKeys, error) {
	if options.Target.Validate() != nil {
		return CacheKeys{}, fmt.Errorf("media cache key requires a valid public target")
	}
	repositoryRoot, err := filepath.Abs(options.RepositoryRoot)
	if err != nil {
		return CacheKeys{}, err
	}
	if !repositoryMarker(repositoryRoot) {
		return CacheKeys{}, fmt.Errorf("media cache key requires an open-cut repository root")
	}

	// The catalog and the build recipe both live in this package, and both
	// decide what the archives and the C toolchain are. Hashing the package
	// over-approximates - an unrelated edit here invalidates too - but it can
	// never under-approximate, and it changes rarely.
	//
	// toolchainclosure is hashed with it because pinned-source acquisition and
	// archive extraction live there. Leaving it out would under-approximate:
	// a change to how archives are unpacked would not invalidate a cache whose
	// contents that change produced. Note it deliberately does not include
	// whispertoolchain - that closure now has its own identity, and a whisper
	// change no longer has any business invalidating the media caches.
	catalog, err := hashDirectories(
		filepath.Join(repositoryRoot, "internal", "mediatoolchain"),
		filepath.Join(repositoryRoot, "internal", "toolchainclosure"),
	)
	if err != nil {
		return CacheKeys{}, fmt.Errorf("hash media catalog: %w", err)
	}
	renderer, err := RendererSourceFingerprint(ctx, repositoryRoot)
	if err != nil {
		return CacheKeys{}, err
	}
	goTool, err := tool.Resolve("go")
	if err != nil {
		return CacheKeys{}, err
	}
	goVersion, err := rendererGoVersion(ctx, goTool)
	if err != nil {
		return CacheKeys{}, err
	}

	sourcePrefix := "media-sources-v1-" + options.Target.String() + "-"
	// The compiled C tree sits between the two: the renderer cannot affect it,
	// but the build environment can, because identical sources compiled against
	// a different system compiler are different objects.
	cbuildPrefix := "media-cbuild-v1-" + options.Target.String() + "-"
	closurePrefix := "media-closure-v1-" + options.Target.String() + "-"
	return CacheKeys{
		Schema: 1, Target: options.Target.String(),
		SourcePrefix:  sourcePrefix,
		SourceKey:     sourcePrefix + shortDigest(toolchainVersion, catalog),
		CBuildPrefix:  cbuildPrefix,
		CBuildKey:     cbuildPrefix + shortDigest(toolchainVersion, catalog, options.Environment),
		ClosurePrefix: closurePrefix,
		ClosureKey: closurePrefix + shortDigest(
			toolchainVersion, catalog, renderer, goVersion, options.Environment,
		),
	}, nil
}

func shortDigest(parts ...string) string {
	digest := sha256.New()
	for _, part := range parts {
		digest.Write([]byte(part))
		digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))[:32]
}

func hashDirectories(roots ...string) (string, error) {
	return toolchainclosure.HashDirectories(roots...)
}
