package mediatoolchain

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/PerishCode/open-cut/utils/tool"
)

func TestNativeTextSourcesArePinned(t *testing.T) {
	sources := nativeTextSourceRecords()
	if len(sources) != 3 || sources[0].ID != "freetype" || sources[1].ID != "fribidi" ||
		sources[2].ID != "harfbuzz" {
		t.Fatalf("sources=%+v", sources)
	}
	for _, source := range sources {
		if !validDigest(source.SHA256) {
			t.Fatalf("source=%+v", source)
		}
	}
}

func TestPinnedNativeTextArchivesBuildStaticClosure(t *testing.T) {
	assetRoot := os.Getenv("OPEN_CUT_RENDERER_ASSET_ROOT")
	if assetRoot == "" {
		t.Skip("OPEN_CUT_RENDERER_ASSET_ROOT is not configured")
	}
	archives := make(map[string]string, 3)
	for _, source := range nativeTextSourceRecords() {
		filename := filepath.Join(assetRoot, filepath.Base(source.URL))
		if digest, _, err := digestFile(filename); err != nil || digest != source.SHA256 {
			t.Fatalf("source=%s digest=%s err=%v", source.ID, digest, err)
		}
		archives[source.ID] = filename
	}
	buildRoot := t.TempDir()
	roots, err := extractNativeTextSources(archives, filepath.Join(buildRoot, "source"))
	if err != nil {
		t.Fatal(err)
	}
	compiler := mustResolveNativeTool(t, "cc")
	cxx := mustResolveNativeTool(t, "c++")
	archiver := mustResolveNativeTool(t, "ar")
	makeTool := mustResolveNativeTool(t, "make")
	if identity, err := inspectBuildTools(context.Background(), compiler, cxx, archiver, makeTool); err != nil || identity == "" {
		t.Fatalf("build tool identity=%q err=%v", identity, err)
	}
	parallelism := min(runtime.NumCPU(), 8)
	recipe, err := buildStaticNativeTextDependencies(
		context.Background(), roots, filepath.Join(buildRoot, "deps"),
		compiler, cxx, archiver, makeTool, parallelism, io.Discard, io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(recipe.FreeType) == 0 || len(recipe.FriBidi) == 0 || len(recipe.HarfBuzz) == 0 {
		t.Fatalf("recipe=%+v", recipe)
	}
	for _, library := range []string{"libfreetype.a", "libfribidi.a", "libharfbuzz.a"} {
		if err := verifyNativeArchive(filepath.Join(buildRoot, "deps", "lib", library)); err != nil {
			t.Fatal(err)
		}
	}
}

func mustResolveNativeTool(t *testing.T, name string) string {
	t.Helper()
	resolved, err := tool.Resolve(name)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
