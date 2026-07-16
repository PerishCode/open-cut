package mediatoolchain

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/utils/target"
	"github.com/PerishCode/open-cut/utils/tool"
	"github.com/klauspost/compress/zstd"
)

func VerifyRendererRelink(ctx context.Context, verified Verified) error {
	record := verified.Manifest.Build.Renderer
	if record == nil || validateRendererBuildRecord(record) != nil || verified.Manifest.Target != target.Host() {
		return fmt.Errorf("renderer relink verification input is invalid")
	}
	notice := noticeRecord(verified.Manifest.Notices, record.RelinkNoticeID)
	if notice.ID == "" {
		return fmt.Errorf("renderer relink archive notice is unavailable")
	}
	root, err := os.MkdirTemp("", "open-cut-render-relink-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	archivePath := filepath.Join(verified.Root, filepath.FromSlash(notice.Path))
	if err := extractRendererRelinkArchive(archivePath, root); err != nil {
		return err
	}
	encoded, err := os.ReadFile(filepath.Join(root, "relink.json"))
	if err != nil || len(encoded) == 0 || len(encoded) > 256<<10 {
		return fmt.Errorf("renderer relink manifest is unavailable")
	}
	var archived RendererBuildRecord
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&archived); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		!reflect.DeepEqual(archived, *record) {
		return fmt.Errorf("renderer relink manifest does not match the catalog")
	}
	instructions, err := os.ReadFile(filepath.Join(root, "REBUILD.md"))
	if err != nil || string(instructions) != rendererRelinkInstructions {
		return fmt.Errorf("renderer relink instructions are invalid")
	}
	sourceRoot := filepath.Join(root, "source")
	files, sourceDigest, err := rendererKitSourceClosure(sourceRoot)
	if err != nil || sourceDigest != record.SourceSHA256 || uint32(len(files)) != record.SourceFileCount {
		return fmt.Errorf("renderer relink source closure is invalid")
	}
	nativeRoot := filepath.Join(root, "native")
	for _, input := range record.LinkInputs {
		digest, size, err := digestFile(filepath.Join(root, filepath.FromSlash(input.Path)))
		if err != nil || digest != input.SHA256 || size != input.ByteSize {
			return fmt.Errorf("renderer relink input %s is invalid", input.ID)
		}
	}
	for _, source := range nativeTextSourceRecords() {
		suffix, err := sourceArchiveSuffix(source.URL)
		if err != nil {
			return err
		}
		digest, _, err := digestFile(filepath.Join(
			nativeRoot, "source", source.ID+"-"+source.Version+suffix,
		))
		if err != nil || digest != source.SHA256 {
			return fmt.Errorf("renderer corresponding source %s is invalid", source.ID)
		}
	}
	goTool, err := tool.Resolve("go")
	if err != nil {
		return err
	}
	goVersion, err := rendererGoVersion(ctx, goTool)
	if err != nil || goVersion != record.GoVersion {
		return fmt.Errorf("renderer relink Go toolchain is unavailable")
	}
	baseline := filepath.Join(root, verified.Manifest.Target.ExecutableName("open-cut-render-baseline-check"))
	if err := buildRendererFromRelinkKit(
		ctx, goTool, sourceRoot, nativeRoot, baseline, io.Discard, io.Discard,
	); err != nil {
		return err
	}
	baselineDigest, baselineSize, err := digestFile(baseline)
	if err != nil {
		return fmt.Errorf("digest renderer baseline relink evidence: %w", err)
	}
	if baselineDigest != record.BaselineRelinkSHA256 || baselineSize != record.BaselineRelinkByteSize {
		return fmt.Errorf(
			"renderer baseline relink evidence is invalid: helper %s/%d want %s/%d",
			baselineDigest, baselineSize, record.BaselineRelinkSHA256, record.BaselineRelinkByteSize,
		)
	}
	if err := os.Remove(baseline); err != nil {
		return err
	}
	fontRecord := resourceRecord(verified.Manifest.Resources, renderengine.CaptionFontBundleID)
	font, exists := verified.Resources[renderengine.CaptionFontBundleID]
	ffmpeg, ffmpegExists := verified.Tools["ffmpeg"]
	if fontRecord.ID == "" || !exists || !ffmpegExists {
		return fmt.Errorf("renderer relink smoke closure is unavailable")
	}
	modifiedDigest, modifiedSize, modifiedFriBidiSource, observation, err := qualifyModifiedRendererRelink(
		ctx, root, sourceRoot, nativeRoot, verified.Manifest.Target, record.BaselineRelinkSHA256,
		RendererSmokeInput{
			FFmpegPath: ffmpeg.Path, FFmpegSHA256: ffmpeg.SHA256,
			FontRoot: font.Root, Font: fontRecord,
		}, io.Discard, io.Discard,
	)
	if err != nil {
		return fmt.Errorf("renderer modified-library relink failed: %w", err)
	}
	if modifiedDigest != record.ModifiedRelinkSHA256 || modifiedSize != record.ModifiedRelinkByteSize ||
		modifiedFriBidiSource != record.ModifiedFriBidiSourceSHA256 ||
		observation.OutputSHA256 != record.SmokeOutputSHA256 ||
		observation.OutputBytes != record.SmokeOutputByteSize {
		return fmt.Errorf(
			"renderer modified-library relink evidence is invalid: helper %s/%d want %s/%d, "+
				"fribidi %s want %s, smoke %s/%d want %s/%d",
			modifiedDigest, modifiedSize, record.ModifiedRelinkSHA256, record.ModifiedRelinkByteSize,
			modifiedFriBidiSource, record.ModifiedFriBidiSourceSHA256,
			observation.OutputSHA256, observation.OutputBytes,
			record.SmokeOutputSHA256, record.SmokeOutputByteSize,
		)
	}
	return nil
}

func extractRendererRelinkArchive(archive, destination string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder, err := zstd.NewReader(file)
	if err != nil {
		return err
	}
	defer decoder.Close()
	reader := tar.NewReader(decoder)
	var total uint64
	for entries := 0; ; entries++ {
		if entries > 65_536 {
			return fmt.Errorf("renderer relink archive has too many entries")
		}
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(header.Name, "/")
		if name == "" || strings.ContainsRune(name, '\\') || path.IsAbs(name) ||
			path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("renderer relink archive path is invalid")
		}
		filename := filepath.Join(destination, filepath.FromSlash(name))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(filename, 0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || uint64(header.Size) > maximumRendererKitBytes-total {
				return fmt.Errorf("renderer relink archive exceeds its bound")
			}
			if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
				return err
			}
			output, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			written, copyErr := io.CopyN(output, reader, header.Size)
			closeErr := output.Close()
			if copyErr != nil || written != header.Size {
				return fmt.Errorf("extract renderer relink archive: %w", copyErr)
			}
			if closeErr != nil {
				return closeErr
			}
			total += uint64(header.Size)
		default:
			return fmt.Errorf("renderer relink archive entry type is invalid")
		}
	}
	return nil
}

func noticeRecord(records []NoticeRecord, id string) NoticeRecord {
	for _, record := range records {
		if record.ID == id {
			return record
		}
	}
	return NoticeRecord{}
}

func resourceRecord(records []ResourceRecord, id string) ResourceRecord {
	for _, record := range records {
		if record.ID == id {
			return record
		}
	}
	return ResourceRecord{}
}
