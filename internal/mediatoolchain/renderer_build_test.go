package mediatoolchain

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/utils/target"
)

func TestRendererBuildNormalizationRemovesPhysicalRoots(t *testing.T) {
	values := []string{
		"-I/work/dependencies/include", "-ffile-prefix-map=/work/repository=.",
	}
	normalized := normalizeRendererBuildValues(values, map[string]string{
		"/work/repository": "$source", "/work/dependencies": "$native",
	})
	if !reflect.DeepEqual(normalized, []string{
		"-I$native/include", "-ffile-prefix-map=$source=.",
	}) {
		t.Fatalf("normalized=%v", normalized)
	}
	for _, value := range normalized {
		if strings.Contains(value, "/work/") {
			t.Fatalf("physical root survived: %q", value)
		}
	}
}

func TestPinnedRendererHelperBuildIsReproducibleAndStatic(t *testing.T) {
	fontRoot := os.Getenv("OPEN_CUT_NATIVE_TEXT_FONT_ROOT")
	if fontRoot == "" {
		t.Skip("pinned native text build fixture is unavailable")
	}
	stageRoot := filepath.Dir(filepath.Dir(filepath.Dir(fontRoot)))
	workspace := filepath.Dir(stageRoot)
	buildRoot := filepath.Join(workspace, "build")
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repositoryRoot, err = filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	result, err := buildRendererHelper(
		context.Background(), repositoryRoot, buildRoot,
		filepath.Join(buildRoot, "dependencies"),
		filepath.Join(buildRoot, "harfbuzz-"+HarfBuzzSourceVersion),
		target.Host(), io.Discard, io.Discard,
	)
	if err != nil || result.Path == "" || result.ByteSize == 0 || len(result.LinkInputs) != 3 ||
		!strings.HasPrefix(result.SHA256, "sha256:") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(stageRoot, ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	var stagedManifest Manifest
	if err := json.Unmarshal(manifestBytes, &stagedManifest); err != nil {
		t.Fatal(err)
	}
	var fontRecord ResourceRecord
	for _, resource := range stagedManifest.Resources {
		if resource.ID == "open-cut-caption-font-v1" {
			fontRecord = resource
		}
	}
	font := Resource{
		ID: fontRecord.ID, Kind: fontRecord.Kind, Version: fontRecord.Version,
		Root: filepath.Join(stageRoot, filepath.FromSlash(fontRecord.Root)), SHA256: fontRecord.SHA256,
	}
	var ffmpegRecord ToolRecord
	for _, current := range stagedManifest.Tools {
		if current.ID == "ffmpeg" {
			ffmpegRecord = current
		}
	}
	ffmpeg := Tool{
		ID: ffmpegRecord.ID, Path: filepath.Join(stageRoot, filepath.FromSlash(ffmpegRecord.Path)),
		SHA256: ffmpegRecord.SHA256, ByteSize: ffmpegRecord.ByteSize,
	}
	archives := make(map[string]string, len(nativeTextSourceRecords()))
	for _, source := range nativeTextSourceRecords() {
		archive, err := sourceArchivePath(workspace, source)
		if err != nil {
			t.Fatal(err)
		}
		archives[source.ID] = archive
	}
	kit, err := buildRendererRelinkKit(
		context.Background(), repositoryRoot, buildRoot,
		filepath.Join(buildRoot, "dependencies"),
		filepath.Join(buildRoot, "harfbuzz-"+HarfBuzzSourceVersion),
		target.Host(), result, archives, RendererSmokeInput{
			FFmpegPath: ffmpeg.Path, FFmpegSHA256: ffmpeg.SHA256,
			FontRoot: font.Root, Font: fontRecord,
		}, io.Discard, io.Discard,
	)
	if err != nil || kit.Root == "" || len(kit.Files) == 0 ||
		!strings.HasPrefix(kit.SourceSHA256, "sha256:") ||
		!strings.HasPrefix(kit.BaselineRelinkSHA256, "sha256:") || kit.BaselineRelinkByteSize == 0 ||
		!strings.HasPrefix(kit.ModifiedRelinkSHA256, "sha256:") || kit.ModifiedRelinkByteSize == 0 ||
		kit.ModifiedRelinkSHA256 == kit.BaselineRelinkSHA256 ||
		!strings.HasPrefix(kit.ModifiedFriBidiSourceSHA256, "sha256:") || kit.Smoke.OutputBytes == 0 {
		t.Fatalf("kit=%+v err=%v", kit, err)
	}
	record := newRendererBuildRecord(result, kit)
	archiveStage, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	notice, err := stageRendererRelinkArchive(archiveStage, kit, record)
	if err != nil {
		t.Fatal(err)
	}
	relinkVerified := Verified{
		Manifest: Manifest{
			Target: target.Host(), Build: BuildRecord{Renderer: &record},
			Resources: []ResourceRecord{fontRecord}, Notices: []NoticeRecord{notice},
		},
		Root: archiveStage, Tools: map[string]Tool{"ffmpeg": ffmpeg},
		Resources: map[string]Resource{font.ID: font},
	}
	if err := VerifyRendererRelink(context.Background(), relinkVerified); err != nil {
		t.Fatal(err)
	}
}
