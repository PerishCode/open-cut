package toolchainclosure

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ulikunitz/xz"
)

const (
	maximumSourceBytes          = int64(512 << 20)
	maximumSourceEntryBytes     = int64(512 << 20)
	maximumSourceExtractedBytes = int64(2 << 30)
)

type ArchiveSelection struct {
	Member      string
	Destination string
}

type ArchiveIgnoredLink struct {
	Member string
	Target string
}

func SourceArchivePath(root string, source SourceRecord) (string, error) {
	suffix, err := SourceArchiveSuffix(source.URL)
	if err != nil {
		return "", fmt.Errorf("pinned %s source archive type: %w", source.ID, err)
	}
	return filepath.Join(root, "source", source.ID+"-"+source.Version+suffix), nil
}

func SourceArchiveSuffix(sourceURL string) (string, error) {
	for _, suffix := range []string{".tar.gz", ".tar.xz", ".zip"} {
		if strings.HasSuffix(sourceURL, suffix) {
			return suffix, nil
		}
	}
	return "", fmt.Errorf("unsupported archive suffix")
}

func ExtractSource(archive, destination, prefix, requiredFile string) (string, error) {
	return ExtractSourceIgnoringLinks(archive, destination, prefix, requiredFile, nil)
}

func ExtractSourceIgnoringLinks(
	archive, destination, prefix, requiredFile string,
	ignoredLinks []ArchiveIgnoredLink,
) (string, error) {
	reader, closeReader, err := openTarArchive(archive)
	if err != nil {
		return "", err
	}
	defer closeReader()

	prefix = path.Clean(prefix)
	if !validArchiveRelative(prefix) || !validArchiveRelative(requiredFile) {
		return "", fmt.Errorf("pinned source extraction contract is invalid")
	}
	ignored := make(map[string]string, len(ignoredLinks))
	for _, link := range ignoredLinks {
		if !validArchiveRelative(link.Member) || !validArchiveRelative(link.Target) {
			return "", fmt.Errorf("pinned source ignored link contract is invalid")
		}
		if _, duplicate := ignored[link.Member]; duplicate {
			return "", fmt.Errorf("pinned source ignored link contract repeats a member")
		}
		ignored[link.Member] = link.Target
	}
	seenIgnored := make(map[string]struct{}, len(ignored))
	var extracted int64
	tarReader := tar.NewReader(reader)
	for {
		header, nextErr := tarReader.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return "", fmt.Errorf("extract pinned source: %w", nextErr)
		}
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		archiveName := header.Name
		if header.Typeflag == tar.TypeDir {
			archiveName = strings.TrimSuffix(archiveName, "/")
		}
		clean := path.Clean(archiveName)
		if !validArchiveRelative(archiveName) || clean != prefix && !strings.HasPrefix(clean, prefix+"/") {
			return "", fmt.Errorf("pinned source contains an escaping entry %q outside %q", clean, prefix)
		}
		relative := strings.TrimPrefix(strings.TrimPrefix(clean, prefix), "/")
		targetPath, err := containedArchiveDestination(destination, path.Join(prefix, relative))
		if err != nil {
			return "", err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0o700); err != nil {
				return "", err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > maximumSourceEntryBytes ||
				extracted > maximumSourceExtractedBytes-header.Size {
				return "", fmt.Errorf("pinned source extraction exceeds its bound")
			}
			extracted += header.Size
			mode := os.FileMode(0o600)
			if header.Mode&0o111 != 0 {
				mode = 0o700
			}
			if err := writeArchiveFile(targetPath, mode, tarReader, header.Size); err != nil {
				return "", err
			}
			if !header.ModTime.IsZero() {
				if err := os.Chtimes(targetPath, header.ModTime, header.ModTime); err != nil {
					return "", fmt.Errorf("restore pinned source entry time: %w", err)
				}
			}
		case tar.TypeSymlink, tar.TypeLink:
			target, allowed := ignored[clean]
			if !allowed || header.Linkname != target {
				return "", fmt.Errorf("pinned source contains a linked or special entry")
			}
			seenIgnored[clean] = struct{}{}
		default:
			return "", fmt.Errorf("pinned source contains a linked or special entry")
		}
	}
	if len(seenIgnored) != len(ignored) {
		return "", fmt.Errorf("pinned source ignored link contract is incomplete")
	}
	root := filepath.Join(destination, filepath.FromSlash(prefix))
	required, err := containedArchiveDestination(root, requiredFile)
	if err != nil {
		return "", err
	}
	if info, err := os.Lstat(required); err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("pinned source is incomplete")
	}
	return root, nil
}

func ExtractZipFiles(archive, destination string, selections []ArchiveSelection) error {
	if len(selections) == 0 || len(selections) > 128 {
		return fmt.Errorf("pinned zip selection is invalid")
	}
	wanted := make(map[string]string, len(selections))
	for _, selection := range selections {
		if !validArchiveRelative(selection.Member) || !validArchiveRelative(selection.Destination) {
			return fmt.Errorf("pinned zip selection path is invalid")
		}
		if _, duplicate := wanted[selection.Member]; duplicate {
			return fmt.Errorf("pinned zip selection repeats a member")
		}
		wanted[selection.Member] = selection.Destination
	}
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("open pinned zip source: %w", err)
	}
	defer reader.Close()
	seen := make(map[string]struct{}, len(wanted))
	var extracted int64
	for _, member := range reader.File {
		destinationRelative, selected := wanted[member.Name]
		if !selected {
			continue
		}
		if _, duplicate := seen[member.Name]; duplicate || !validArchiveRelative(member.Name) ||
			!member.Mode().IsRegular() || member.Mode()&os.ModeSymlink != 0 ||
			member.UncompressedSize64 == 0 || member.UncompressedSize64 > uint64(maximumSourceEntryBytes) ||
			member.UncompressedSize64 > uint64(maximumSourceExtractedBytes-extracted) {
			return fmt.Errorf("selected pinned zip member is invalid")
		}
		extracted += int64(member.UncompressedSize64)
		output, err := containedArchiveDestination(destination, destinationRelative)
		if err != nil {
			return err
		}
		input, err := member.Open()
		if err != nil {
			return err
		}
		writeErr := writeArchiveFile(output, 0o600, input, int64(member.UncompressedSize64))
		closeErr := input.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
		seen[member.Name] = struct{}{}
	}
	if len(seen) != len(wanted) {
		return fmt.Errorf("pinned zip source is missing a selected member")
	}
	return nil
}

func openTarArchive(filename string) (io.Reader, func() error, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, func() error { return nil }, err
	}
	closeReader := file.Close
	switch {
	case strings.HasSuffix(filename, ".tar.gz"):
		compressed, err := gzip.NewReader(file)
		if err != nil {
			file.Close()
			return nil, func() error { return nil }, fmt.Errorf("open pinned gzip source: %w", err)
		}
		return compressed, func() error {
			first := compressed.Close()
			if second := file.Close(); first == nil {
				return second
			}
			return first
		}, nil
	case strings.HasSuffix(filename, ".tar.xz"):
		compressed, err := xz.NewReader(file)
		if err != nil {
			file.Close()
			return nil, func() error { return nil }, fmt.Errorf("open pinned xz source: %w", err)
		}
		return compressed, closeReader, nil
	default:
		file.Close()
		return nil, func() error { return nil }, fmt.Errorf("pinned source is not a supported tar archive")
	}
}

func validArchiveRelative(value string) bool {
	return value != "" && value == path.Clean(value) && value != "." && !path.IsAbs(value) &&
		value != ".." && !strings.HasPrefix(value, "../") && !strings.Contains(value, "\\")
}

func containedArchiveDestination(root, relative string) (string, error) {
	if !validArchiveRelative(relative) {
		return "", fmt.Errorf("pinned source destination is invalid")
	}
	targetPath := filepath.Join(root, filepath.FromSlash(relative))
	contained, err := filepath.Rel(root, targetPath)
	if err != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(contained) {
		return "", fmt.Errorf("pinned source entry escapes extraction root")
	}
	return targetPath, nil
}

func writeArchiveFile(destination string, mode os.FileMode, source io.Reader, size int64) error {
	if size < 0 || size > maximumSourceEntryBytes {
		return fmt.Errorf("pinned source entry size is invalid")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(output, io.LimitReader(source, size+1))
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil || written != size {
		_ = os.Remove(destination)
		return fmt.Errorf("extract pinned source entry")
	}
	return nil
}
