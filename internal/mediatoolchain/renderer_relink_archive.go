package mediatoolchain

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/klauspost/compress/zstd"
)

const rendererRelinkArchiveRelative = "licenses/media/OPEN-CUT-RENDER-RELINK.tar.zst"

var rendererArchiveEpoch = time.Unix(0, 0).UTC()

func stageRendererRelinkArchive(
	stageRoot string,
	kit RendererRelinkKit,
	record RendererBuildRecord,
) (NoticeRecord, error) {
	if !cleanAbsolute(stageRoot) || !cleanAbsolute(kit.Root) || validateRendererBuildRecord(&record) != nil {
		return NoticeRecord{}, fmt.Errorf("renderer relink archive input is invalid")
	}
	for _, generated := range []string{"qualification", ".gocache"} {
		if err := os.RemoveAll(filepath.Join(kit.Root, generated)); err != nil {
			return NoticeRecord{}, err
		}
	}
	if err := atomicfile.WriteJSON(filepath.Join(kit.Root, "relink.json"), record, 0o600); err != nil {
		return NoticeRecord{}, err
	}
	if err := atomicfile.Write(
		filepath.Join(kit.Root, "REBUILD.md"), []byte(rendererRelinkInstructions), 0o600,
	); err != nil {
		return NoticeRecord{}, err
	}
	destination := filepath.Join(stageRoot, filepath.FromSlash(rendererRelinkArchiveRelative))
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return NoticeRecord{}, err
	}
	first := destination + ".first"
	second := destination + ".second"
	defer os.Remove(first)
	defer os.Remove(second)
	if err := packRendererRelinkTree(kit.Root, first); err != nil {
		return NoticeRecord{}, err
	}
	if err := packRendererRelinkTree(kit.Root, second); err != nil {
		return NoticeRecord{}, err
	}
	firstDigest, firstSize, err := digestFile(first)
	if err != nil {
		return NoticeRecord{}, err
	}
	secondDigest, secondSize, err := digestFile(second)
	if err != nil || secondDigest != firstDigest || secondSize != firstSize {
		return NoticeRecord{}, fmt.Errorf("renderer relink archive is not byte reproducible")
	}
	if err := os.Rename(first, destination); err != nil {
		return NoticeRecord{}, err
	}
	return NoticeRecord{
		ID: RendererRelinkNoticeID, Path: rendererRelinkArchiveRelative,
		SHA256: firstDigest, ByteSize: firstSize,
	}, nil
}

func packRendererRelinkTree(sourceRoot, destination string) error {
	names := make([]string, 0)
	var total uint64
	err := filepath.WalkDir(sourceRoot, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filename == sourceRoot {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("renderer relink archive contains a linked entry")
		}
		relative, err := filepath.Rel(sourceRoot, filename)
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.Size() < 0 ||
				uint64(info.Size()) > maximumRendererKitBytes-total {
				return fmt.Errorf("renderer relink archive exceeds its bound")
			}
			total += uint64(info.Size())
		}
		names = append(names, relative)
		return nil
	})
	if err != nil {
		return err
	}
	slices.Sort(names)
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(destination)
		}
	}()
	encoder, err := zstd.NewWriter(
		file, zstd.WithEncoderLevel(zstd.SpeedBetterCompression), zstd.WithEncoderConcurrency(1),
	)
	if err != nil {
		return err
	}
	tarWriter := tar.NewWriter(encoder)
	for _, relative := range names {
		filename := filepath.Join(sourceRoot, relative)
		info, err := os.Lstat(filename)
		if err != nil {
			return err
		}
		header := &tar.Header{
			Name: filepath.ToSlash(relative), ModTime: rendererArchiveEpoch,
			AccessTime: rendererArchiveEpoch, ChangeTime: rendererArchiveEpoch,
			Uid: 0, Gid: 0, Uname: "", Gname: "",
		}
		if info.IsDir() {
			header.Typeflag, header.Mode = tar.TypeDir, 0o700
			header.Name = strings.TrimSuffix(header.Name, "/") + "/"
		} else {
			header.Typeflag, header.Mode, header.Size = tar.TypeReg, 0o600, info.Size()
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			continue
		}
		input, err := os.Open(filename)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tarWriter, input)
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	success = true
	return nil
}

const rendererRelinkInstructions = `# Open Cut renderer relink material

This target-specific kit contains the minimal Open Cut Go module source,
vendored module dependencies, native headers, the original static link inputs,
and the corresponding pinned native source archives needed to rebuild
open-cut-render with a modified LGPL FriBidi library.

Use the exact Go version and normalized build arguments in relink.json. Build
from source/ with CGO enabled, -mod=vendor, the open_cut_renderer_native tag,
include paths below native/include, and the library path native/lib. Replace
native/lib/libfribidi.a with a compatible rebuilt archive, then run the same Go
build command. open-cut-render remains a private payload executable; these
instructions do not install it, add it to PATH, or create an operations entry.
`
