package bundle

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

var archiveEpoch = time.Unix(0, 0).UTC()

func Pack(sourceRoot, destination string) error {
	for _, required := range []string{"manifest.json", "launcher", "payload"} {
		if _, err := os.Stat(filepath.Join(sourceRoot, required)); err != nil {
			return fmt.Errorf("bundle source requires %s: %w", required, err)
		}
	}

	var names []string
	if err := filepath.WalkDir(sourceRoot, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == sourceRoot {
			return nil
		}
		relative, err := filepath.Rel(sourceRoot, name)
		if err != nil {
			return err
		}
		names = append(names, relative)
		return nil
	}); err != nil {
		return fmt.Errorf("walk bundle source: %w", err)
	}
	sort.Strings(names)

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create bundle: %w", err)
	}
	success := false
	defer func() {
		file.Close()
		if !success {
			os.Remove(destination)
		}
	}()

	encoder, err := zstd.NewWriter(file, zstd.WithEncoderLevel(zstd.SpeedBetterCompression), zstd.WithEncoderConcurrency(1))
	if err != nil {
		return fmt.Errorf("create zstd encoder: %w", err)
	}
	tarWriter := tar.NewWriter(encoder)
	for _, relative := range names {
		fullPath := filepath.Join(sourceRoot, relative)
		info, err := os.Lstat(fullPath)
		if err != nil {
			return err
		}
		linkTarget := ""
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err = os.Readlink(fullPath)
			if err != nil {
				return err
			}
			if err := validateSourceLink(sourceRoot, fullPath, linkTarget); err != nil {
				return err
			}
			linkTarget = filepath.ToSlash(linkTarget)
		}
		header, err := tar.FileInfoHeader(info, linkTarget)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		header.ModTime = archiveEpoch
		header.AccessTime = archiveEpoch
		header.ChangeTime = archiveEpoch
		header.Uid, header.Gid = 0, 0
		header.Uname, header.Gname = "", ""
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		input, err := os.Open(fullPath)
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

func Extract(source, destination string) error {
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		if err == nil {
			return fmt.Errorf("extraction destination already exists")
		}
		return err
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(destination)
		}
	}()

	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder, err := zstd.NewReader(file)
	if err != nil {
		return fmt.Errorf("create zstd decoder: %w", err)
	}
	defer decoder.Close()
	tarReader := tar.NewReader(decoder)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}
		relative, err := safeArchivePath(header.Name)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, filepath.FromSlash(relative))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := ensureDirectory(destination, relative, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := ensureDirectory(destination, path.Dir(relative), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if header.FileInfo().Mode()&0o111 != 0 {
				mode = 0o755
			}
			output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return fmt.Errorf("create extracted file %s: %w", relative, err)
			}
			_, copyErr := io.CopyN(output, tarReader, header.Size)
			closeErr := output.Close()
			if copyErr != nil {
				return fmt.Errorf("extract %s: %w", relative, copyErr)
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink:
			if err := validateArchiveLink(relative, header.Linkname); err != nil {
				return err
			}
			if err := ensureDirectory(destination, path.Dir(relative), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(filepath.FromSlash(header.Linkname), target); err != nil {
				return fmt.Errorf("create extracted symlink %s: %w", relative, err)
			}
		default:
			return fmt.Errorf("unsupported tar entry type %d for %s", header.Typeflag, relative)
		}
	}
	success = true
	return nil
}

func SHA256(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func ReadFile(source, requested string, maxBytes int64) ([]byte, error) {
	clean, err := safeArchivePath(requested)
	if err != nil || clean != requested {
		return nil, fmt.Errorf("invalid requested bundle path %q", requested)
	}
	file, err := os.Open(source)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder, err := zstd.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("create zstd decoder: %w", err)
	}
	defer decoder.Close()
	reader := tar.NewReader(decoder)
	for {
		header, nextErr := reader.Next()
		if nextErr == io.EOF {
			return nil, fmt.Errorf("bundle does not contain %s", requested)
		}
		if nextErr != nil {
			return nil, nextErr
		}
		if header.Name != requested {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("bundle entry %s is not a regular file", requested)
		}
		if header.Size < 0 || header.Size > maxBytes {
			return nil, fmt.Errorf("bundle entry %s exceeds %d bytes", requested, maxBytes)
		}
		return io.ReadAll(io.LimitReader(reader, maxBytes+1))
	}
}

func safeArchivePath(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\\') || path.IsAbs(name) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != name {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return clean, nil
}

func ensureDirectory(root, relative string, mode os.FileMode) error {
	if relative == "." || relative == "" {
		return nil
	}
	current := root
	for _, segment := range strings.Split(relative, "/") {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, mode); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive parent %s is not a real directory", current)
		}
	}
	return nil
}

func validateSourceLink(root, linkPath, target string) error {
	if target == "" || filepath.IsAbs(target) {
		return fmt.Errorf("symlink %s has unsafe target %q", linkPath, target)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(linkPath), target))
	relative, err := filepath.Rel(filepath.Clean(root), resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("symlink %s escapes bundle root", linkPath)
	}
	return nil
}

func validateArchiveLink(entry, target string) error {
	if target == "" || path.IsAbs(target) || strings.ContainsRune(target, '\\') {
		return fmt.Errorf("symlink %s has unsafe target %q", entry, target)
	}
	resolved := path.Clean(path.Join(path.Dir(entry), target))
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return fmt.Errorf("symlink %s escapes extraction root", entry)
	}
	return nil
}
