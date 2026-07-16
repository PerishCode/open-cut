package mediatoolchain

import (
	"bytes"
	"context"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/utils/environment"
	"github.com/PerishCode/open-cut/utils/target"
	"github.com/PerishCode/open-cut/utils/tool"
)

const (
	RendererBuildTag     = "open_cut_renderer_native"
	RendererBuildPackage = "./cmd/open-cut-render"
)

type RendererHelperBuild struct {
	Path       string
	SHA256     string
	ByteSize   uint64
	GoVersion  string
	Arguments  []string
	CFlags     []string
	LDFlags    []string
	LinkInputs []RendererLinkInput
}

type RendererLinkInput struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	ByteSize uint64 `json:"byteSize"`
}

func buildRendererHelper(
	ctx context.Context,
	repositoryRoot, buildRoot, dependencyRoot, harfBuzzRoot string,
	buildTarget target.Target,
	stdout, stderr io.Writer,
) (RendererHelperBuild, error) {
	if !cleanAbsolute(repositoryRoot) || !cleanAbsolute(buildRoot) || !cleanAbsolute(dependencyRoot) ||
		!cleanAbsolute(harfBuzzRoot) || buildTarget.Validate() != nil || buildTarget != target.Host() {
		return RendererHelperBuild{}, fmt.Errorf("renderer helper build roots are invalid")
	}
	goTool, err := tool.Resolve("go")
	if err != nil {
		return RendererHelperBuild{}, err
	}
	goVersion, err := rendererGoVersion(ctx, goTool)
	if err != nil {
		return RendererHelperBuild{}, err
	}
	outputRoot := filepath.Join(buildRoot, "renderer-helper")
	if err := os.RemoveAll(outputRoot); err != nil {
		return RendererHelperBuild{}, err
	}
	if err := os.MkdirAll(outputRoot, 0o700); err != nil {
		return RendererHelperBuild{}, err
	}
	includeFlags := []string{
		"-I" + filepath.Join(dependencyRoot, "include", "freetype2"),
		"-I" + filepath.Join(dependencyRoot, "include", "fribidi"),
		"-I" + filepath.Join(harfBuzzRoot, "src"),
		"-ffile-prefix-map=" + repositoryRoot + "=.",
	}
	linkFlags := rendererNativeLinkFlags(dependencyRoot, buildTarget)
	arguments := []string{
		"build", "-buildvcs=false", "-trimpath", "-mod=readonly", "-tags", RendererBuildTag,
		"-ldflags=-buildid=", "-o", "$output", RendererBuildPackage,
	}
	environment := rendererBuildEnvironment(includeFlags, linkFlags)
	paths := []string{
		filepath.Join(outputRoot, buildTarget.ExecutableName("open-cut-render-first")),
		filepath.Join(outputRoot, buildTarget.ExecutableName("open-cut-render-second")),
	}
	var expectedDigest string
	var expectedSize uint64
	for index, output := range paths {
		currentArguments := append([]string(nil), arguments...)
		currentArguments[len(currentArguments)-2] = output
		if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
			Executable: goTool, Args: currentArguments, Directory: repositoryRoot, Env: environment,
			Stdout: stdout, Stderr: stderr, Profile: lifecycle.ProfileDevelopment,
			Presentation: lifecycle.PresentationHeadless,
		}); err != nil {
			return RendererHelperBuild{}, fmt.Errorf("build pinned renderer helper: %w", err)
		}
		if err := verifyRendererDynamicClosure(output); err != nil {
			return RendererHelperBuild{}, err
		}
		digest, size, err := digestFile(output)
		if err != nil {
			return RendererHelperBuild{}, err
		}
		if index == 0 {
			expectedDigest, expectedSize = digest, size
		} else if digest != expectedDigest || size != expectedSize {
			return RendererHelperBuild{}, fmt.Errorf("renderer helper build is not byte reproducible")
		}
	}
	finalPath := filepath.Join(outputRoot, buildTarget.ExecutableName("open-cut-render"))
	if err := os.Rename(paths[0], finalPath); err != nil {
		return RendererHelperBuild{}, err
	}
	if err := os.Remove(paths[1]); err != nil {
		return RendererHelperBuild{}, err
	}
	linkInputs, err := rendererLinkInputs(dependencyRoot)
	if err != nil {
		return RendererHelperBuild{}, err
	}
	return RendererHelperBuild{
		Path: finalPath, SHA256: expectedDigest, ByteSize: expectedSize, GoVersion: goVersion,
		Arguments: append([]string(nil), arguments...),
		CFlags: normalizeRendererBuildValues(includeFlags, map[string]string{
			repositoryRoot: "$source", dependencyRoot: "$native", harfBuzzRoot: "$harfbuzz",
		}),
		LDFlags:    normalizeRendererBuildValues(linkFlags, map[string]string{dependencyRoot: "$native"}),
		LinkInputs: linkInputs,
	}, nil
}

func rendererBuildEnvironment(cflags, ldflags []string) []string {
	return environment.Merge(
		os.Environ(),
		[]string{"GOFLAGS", "GOWORK", "GOENV", "CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_LDFLAGS"},
		map[string]string{
			"CGO_ENABLED": "1", "CGO_CFLAGS": strings.Join(cflags, " "),
			"CGO_LDFLAGS": strings.Join(ldflags, " "),
		},
	)
}

func rendererNativeLinkFlags(dependencyRoot string, buildTarget target.Target) []string {
	flags := []string{"-L" + filepath.Join(dependencyRoot, "lib")}
	if buildTarget.Platform == target.Win {
		flags = append(flags, "-static")
	}
	return flags
}

func rendererGoVersion(ctx context.Context, executable string) (string, error) {
	var stdout, stderr bytes.Buffer
	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: executable, Args: []string{"version"}, Stdout: &stdout, Stderr: &stderr,
		Profile: lifecycle.ProfileDevelopment, Presentation: lifecycle.PresentationHeadless,
	}); err != nil || stdout.Len() == 0 || stdout.Len() > 16<<10 || stderr.Len() > 16<<10 {
		return "", fmt.Errorf("inspect renderer Go toolchain")
	}
	return strings.TrimSpace(stdout.String()), nil
}

func rendererLinkInputs(dependencyRoot string) ([]RendererLinkInput, error) {
	definitions := []struct{ id, filename string }{
		{"freetype", "libfreetype.a"}, {"fribidi", "libfribidi.a"}, {"harfbuzz", "libharfbuzz.a"},
	}
	result := make([]RendererLinkInput, 0, len(definitions))
	for _, definition := range definitions {
		filename := filepath.Join(dependencyRoot, "lib", definition.filename)
		digest, size, err := digestFile(filename)
		if err != nil {
			return nil, fmt.Errorf("inspect renderer link input %s: %w", definition.id, err)
		}
		result = append(result, RendererLinkInput{
			ID: definition.id, Path: "native/lib/" + definition.filename, SHA256: digest, ByteSize: size,
		})
	}
	return result, nil
}

func normalizeRendererBuildValues(values []string, replacements map[string]string) []string {
	keys := make([]string, 0, len(replacements))
	for value := range replacements {
		keys = append(keys, value)
	}
	slices.SortFunc(keys, func(left, right string) int { return len(right) - len(left) })
	result := make([]string, len(values))
	for index, value := range values {
		for _, actual := range keys {
			value = strings.ReplaceAll(value, actual, replacements[actual])
		}
		result[index] = value
	}
	return result
}

func verifyRendererDynamicClosure(filename string) error {
	libraries, err := rendererImportedLibraries(filename)
	if err != nil {
		return fmt.Errorf("inspect renderer dynamic closure: %w", err)
	}
	for _, library := range libraries {
		if reason := forbiddenRendererDynamicLibrary(library); reason != "" {
			return fmt.Errorf("renderer dynamically links %s %s", reason, library)
		}
	}
	return nil
}

func forbiddenRendererDynamicLibrary(library string) string {
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

func rendererImportedLibraries(filename string) ([]string, error) {
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
	return nil, fmt.Errorf("renderer executable format is unsupported")
}
