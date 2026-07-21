package toolchainclosure

import (
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"fmt"
	"path/filepath"
	"strings"
)

// VerifyPackagedExecutableDynamicClosure rejects an executable that would need
// a library the installer does not ship.
//
// It is shared because every source-built executable the product publishes has
// the same obligation, whatever closure produced it: a binary that resolves a
// pinned library from the build host at run time is not the binary that was
// qualified.
func VerifyPackagedExecutableDynamicClosure(filename string) error {
	libraries, err := importedLibraries(filename)
	if err != nil {
		return fmt.Errorf("inspect executable dynamic closure: %w", err)
	}
	for _, library := range libraries {
		if reason := forbiddenPackagedDynamicLibrary(library); reason != "" {
			return fmt.Errorf("packaged executable dynamically links %s %s", reason, library)
		}
	}
	return nil
}

func forbiddenPackagedDynamicLibrary(library string) string {
	lower := strings.ToLower(filepath.Base(filepath.ToSlash(library)))
	if strings.Contains(lower, "harfbuzz") || strings.Contains(lower, "fribidi") ||
		strings.Contains(lower, "freetype") {
		return "pinned native text library"
	}
	for _, prefix := range []string{
		"libgcc_", "libstdc++-", "libwinpthread-", "libssp-", "libgomp-", "libquadmath-",
	} {
		if strings.HasPrefix(lower, prefix) && strings.HasSuffix(lower, ".dll") {
			return "unshipped MinGW runtime library"
		}
	}
	return ""
}

func importedLibraries(filename string) ([]string, error) {
	if current, err := macho.Open(filename); err == nil {
		defer current.Close()
		return current.ImportedLibraries()
	}
	if current, err := elf.Open(filename); err == nil {
		defer current.Close()
		return current.ImportedLibraries()
	}
	if current, err := pe.Open(filename); err == nil {
		defer current.Close()
		return current.ImportedLibraries()
	}
	return nil, fmt.Errorf("executable format is unsupported")
}
