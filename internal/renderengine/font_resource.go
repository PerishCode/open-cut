package renderengine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/PerishCode/open-cut/internal/mediaclosure"
	"github.com/PerishCode/open-cut/product/domain"
)

const (
	CaptionFontResourceKind         = "font-bundle"
	CaptionFontResourceRoot         = "media/fonts/open-cut-caption-font-v1"
	maximumCaptionFontFileBytes     = uint64(128) << 20
	maximumCaptionFontResourceBytes = uint64(512) << 20
)

// VerifyCaptionFontResource closes the private renderer's physical resource
// directory back to the catalog-owned aggregate digest before native code sees
// any font bytes.
func VerifyCaptionFontResource(
	root, expectedID, expectedVersion string,
	expectedSHA256 domain.Digest,
) (CaptionFontBundle, error) {
	if !cleanAbsoluteDirectory(root) || expectedID != CaptionFontBundleID ||
		expectedVersion != CaptionFontBundleVersion || !validDigest(expectedSHA256) {
		return CaptionFontBundle{}, fmt.Errorf("caption font resource identity is invalid")
	}
	manifestPath := filepath.Join(root, CaptionFontBundleFilename)
	manifestInfo, err := os.Lstat(manifestPath)
	if err != nil || !manifestInfo.Mode().IsRegular() || manifestInfo.Mode()&os.ModeSymlink != 0 ||
		manifestInfo.Size() <= 0 || manifestInfo.Size() > MaximumFontBundleBytes {
		return CaptionFontBundle{}, fmt.Errorf("caption font bundle file is invalid")
	}
	encoded, err := os.ReadFile(manifestPath)
	if err != nil {
		return CaptionFontBundle{}, fmt.Errorf("read caption font bundle: %w", err)
	}
	bundle, err := DecodeCaptionFontBundle(encoded)
	if err != nil || bundle.ID != expectedID || bundle.Version != expectedVersion {
		return CaptionFontBundle{}, fmt.Errorf("caption font bundle does not match its execution identity")
	}

	files := make([]mediaclosure.File, 0, len(bundle.Files)+1)
	var total uint64
	for _, declared := range bundle.Files {
		filename, err := containedCaptionFontFile(root, declared.Path)
		if err != nil {
			return CaptionFontBundle{}, err
		}
		info, err := os.Lstat(filename)
		if err != nil || info.Size() <= 0 || uint64(info.Size()) > maximumCaptionFontFileBytes ||
			uint64(info.Size()) > maximumCaptionFontResourceBytes-total {
			return CaptionFontBundle{}, fmt.Errorf("caption font %s exceeds its bound", declared.Path)
		}
		digest, size, err := digestCaptionFontFile(filename)
		if err != nil || digest != declared.SHA256.String() || size != declared.ByteSize {
			return CaptionFontBundle{}, fmt.Errorf("caption font %s does not match its bundle", declared.Path)
		}
		total += size
		files = append(files, mediaclosure.File{Path: declared.Path, SHA256: digest, ByteSize: size})
	}
	manifestDigest, manifestSize, err := digestCaptionFontFile(manifestPath)
	if err != nil || manifestSize > maximumCaptionFontResourceBytes-total {
		return CaptionFontBundle{}, fmt.Errorf("digest caption font bundle")
	}
	files = append(files, mediaclosure.File{
		Path: CaptionFontBundleFilename, SHA256: manifestDigest, ByteSize: manifestSize,
	})
	slices.SortFunc(files, func(left, right mediaclosure.File) int {
		return strings.Compare(left.Path, right.Path)
	})
	if err := verifyCaptionFontTree(root, files); err != nil {
		return CaptionFontBundle{}, err
	}
	actual, err := mediaclosure.ResourceDigest(mediaclosure.Resource{
		ID: expectedID, Kind: CaptionFontResourceKind, Version: expectedVersion,
		Root: CaptionFontResourceRoot, Files: files,
	})
	if err != nil || actual != expectedSHA256.String() {
		return CaptionFontBundle{}, fmt.Errorf("caption font resource closure digest is invalid")
	}
	return bundle, nil
}

func containedCaptionFontFile(root, relative string) (string, error) {
	if !validFontRelative(relative) {
		return "", fmt.Errorf("caption font path is invalid")
	}
	filename := filepath.Join(root, filepath.FromSlash(relative))
	physical, err := filepath.EvalSymlinks(filename)
	if err != nil || filepath.Clean(physical) != filename {
		return "", fmt.Errorf("caption font %s is linked", relative)
	}
	contained, err := filepath.Rel(root, filename)
	if err != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(contained) {
		return "", fmt.Errorf("caption font %s escapes its resource", relative)
	}
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("caption font %s is invalid", relative)
	}
	return filename, nil
}

func verifyCaptionFontTree(root string, declared []mediaclosure.File) error {
	want := make(map[string]struct{}, len(declared))
	for _, file := range declared {
		want[file.Path] = struct{}{}
	}
	seen := make(map[string]struct{}, len(declared))
	err := filepath.WalkDir(root, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filename == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("caption font resource contains a linked entry")
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("caption font resource contains a non-regular entry")
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if _, exists := want[relative]; !exists {
			return fmt.Errorf("caption font resource contains undeclared file %s", relative)
		}
		seen[relative] = struct{}{}
		return nil
	})
	if err != nil {
		return fmt.Errorf("verify caption font resource tree: %w", err)
	}
	if len(seen) != len(want) {
		return fmt.Errorf("caption font resource is missing a declared file")
	}
	return nil
}

func digestCaptionFontFile(filename string) (string, uint64, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	digest := sha256.New()
	written, err := io.Copy(digest, file)
	if err != nil || written <= 0 {
		return "", 0, fmt.Errorf("digest caption font file: %w", err)
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), uint64(written), nil
}
