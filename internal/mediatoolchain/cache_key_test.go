package mediatoolchain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/utils/target"
)

func cacheKeys(t *testing.T, environment string) CacheKeys {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := ComputeCacheKeys(context.Background(), CacheKeyOptions{
		RepositoryRoot: root, Target: target.Host(), Environment: environment,
	})
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

// The two scopes exist to be invalidated by different things. Editing the
// renderer must not discard the pinned archives - they are most of half a
// gigabyte of fonts that no renderer change can affect - and a build
// environment that upgrades its compilers must discard the closure it
// produced, because identical sources compiled elsewhere are different bytes.
func TestCacheKeysSeparateArchivesFromTheBuiltClosure(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	base := cacheKeys(t, "environment-one")

	if elsewhere := cacheKeys(t, "environment-two"); elsewhere.SourceKey != base.SourceKey {
		t.Fatal("a different build environment must not discard the pinned archives")
	} else if elsewhere.ClosureKey == base.ClosureKey {
		t.Fatal("a different build environment must discard the built closure")
	}

	victim := filepath.Join(root, "internal", "renderengine", "oracle.go")
	original, err := os.ReadFile(victim)
	if err != nil {
		t.Skipf("renderer source probe unavailable: %v", err)
	}
	t.Cleanup(func() { _ = os.WriteFile(victim, original, 0o644) })
	if err := os.WriteFile(victim, append(original, []byte("\n// cache key probe\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	edited := cacheKeys(t, "environment-one")
	if edited.SourceKey != base.SourceKey {
		t.Fatal("a renderer edit must not discard the pinned archives")
	}
	if edited.ClosureKey == base.ClosureKey {
		t.Fatal("a renderer edit must discard the built closure")
	}
}

func TestCacheKeysAreStableAndPrefixed(t *testing.T) {
	first := cacheKeys(t, "environment-one")
	again := cacheKeys(t, "environment-one")
	if first != again {
		t.Fatalf("cache keys are not stable: %+v vs %+v", first, again)
	}
	if !strings.HasPrefix(first.SourceKey, first.SourcePrefix) ||
		!strings.HasPrefix(first.ClosureKey, first.ClosurePrefix) {
		t.Fatalf("cache keys must extend their restore prefixes: %+v", first)
	}
	if !strings.Contains(first.SourcePrefix, target.Host().String()) {
		t.Fatalf("cache prefixes must be target scoped: %+v", first)
	}
}
