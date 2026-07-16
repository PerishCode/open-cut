package mediatoolchain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/environment"
	"github.com/PerishCode/open-cut/utils/target"
	"github.com/PerishCode/open-cut/utils/tool"
)

const (
	rendererSourceClosureDomain = "open-cut/renderer-source-closure/v1"
	maximumRendererListBytes    = 32 << 20
	maximumRendererKitBytes     = uint64(512) << 20
)

type RendererRelinkKit struct {
	Root                        string
	SourceRoot                  string
	NativeRoot                  string
	SourceSHA256                string
	BaselineRelinkSHA256        string
	BaselineRelinkByteSize      uint64
	ModifiedRelinkSHA256        string
	ModifiedRelinkByteSize      uint64
	ModifiedFriBidiSourceSHA256 string
	Smoke                       RendererSmokeObservation
	Files                       []RendererSourceFile
}

type RendererSourceFile struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	ByteSize uint64 `json:"byteSize"`
}

type rendererGoPackage struct {
	Dir        string
	ImportPath string
	Standard   bool
	GoFiles    []string
	CgoFiles   []string
	CFiles     []string
	CXXFiles   []string
	HFiles     []string
	SFiles     []string
	EmbedFiles []string
	Module     *rendererGoModule
}

type rendererGoModule struct {
	Path      string
	Version   string
	Dir       string
	GoMod     string
	GoVersion string
	Sum       string
	GoModSum  string
	Main      bool
	Replace   *rendererGoModule
}

func buildRendererRelinkKit(
	ctx context.Context,
	repositoryRoot, buildRoot, dependencyRoot, harfBuzzRoot string,
	buildTarget target.Target,
	release RendererHelperBuild,
	archives map[string]string,
	smoke RendererSmokeInput,
	stdout, stderr io.Writer,
) (RendererRelinkKit, error) {
	if release.Path == "" || release.SHA256 == "" || release.ByteSize == 0 {
		return RendererRelinkKit{}, fmt.Errorf("renderer release build is unavailable")
	}
	kitRoot := filepath.Join(buildRoot, "renderer-relink-kit")
	if err := os.RemoveAll(kitRoot); err != nil {
		return RendererRelinkKit{}, err
	}
	sourceRoot := filepath.Join(kitRoot, "source")
	nativeRoot := filepath.Join(kitRoot, "native")
	if err := os.MkdirAll(sourceRoot, 0o700); err != nil {
		return RendererRelinkKit{}, err
	}
	packages, modules, err := rendererSourceGraph(
		ctx, repositoryRoot, dependencyRoot, harfBuzzRoot, stdout, stderr,
	)
	if err != nil {
		return RendererRelinkKit{}, err
	}
	if err := stageRendererModuleSource(repositoryRoot, sourceRoot, packages, modules); err != nil {
		return RendererRelinkKit{}, err
	}
	if err := stageRendererNativeInputs(dependencyRoot, harfBuzzRoot, nativeRoot); err != nil {
		return RendererRelinkKit{}, err
	}
	if err := stageRendererNativeSources(archives, nativeRoot); err != nil {
		return RendererRelinkKit{}, err
	}
	goTool, err := tool.Resolve("go")
	if err != nil {
		return RendererRelinkKit{}, err
	}
	cflags, ldflags := rendererKitNativeFlags(sourceRoot, nativeRoot)
	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: goTool, Args: []string{"mod", "vendor"}, Directory: sourceRoot,
		Env: rendererBuildEnvironment(cflags, ldflags), Stdout: stdout, Stderr: stderr,
		Profile: lifecycle.ProfileDevelopment, Presentation: lifecycle.PresentationHeadless,
	}); err != nil {
		return RendererRelinkKit{}, fmt.Errorf("vendor renderer relink source: %w", err)
	}
	files, digest, err := rendererKitSourceClosure(sourceRoot)
	if err != nil {
		return RendererRelinkKit{}, err
	}
	var baselineDigest string
	var baselineSize uint64
	for index := 0; index < 2; index++ {
		relinked := filepath.Join(
			kitRoot, buildTarget.ExecutableName(fmt.Sprintf("open-cut-render-baseline-relink-%d", index+1)),
		)
		if err := buildRendererFromRelinkKit(
			ctx, goTool, sourceRoot, nativeRoot, relinked, stdout, stderr,
		); err != nil {
			return RendererRelinkKit{}, err
		}
		relinkedDigest, relinkedSize, err := digestFile(relinked)
		if err != nil {
			return RendererRelinkKit{}, err
		}
		if index == 0 {
			baselineDigest, baselineSize = relinkedDigest, relinkedSize
		} else if relinkedDigest != baselineDigest || relinkedSize != baselineSize {
			return RendererRelinkKit{}, fmt.Errorf("baseline renderer relink is not byte reproducible")
		}
		if err := os.Remove(relinked); err != nil {
			return RendererRelinkKit{}, err
		}
	}
	modifiedDigest, modifiedSize, modifiedFriBidiSource, observation, err := qualifyModifiedRendererRelink(
		ctx, kitRoot, sourceRoot, nativeRoot, buildTarget, baselineDigest, smoke, stdout, stderr,
	)
	if err != nil {
		return RendererRelinkKit{}, err
	}
	return RendererRelinkKit{
		Root: kitRoot, SourceRoot: sourceRoot, NativeRoot: nativeRoot,
		SourceSHA256: digest, BaselineRelinkSHA256: baselineDigest,
		BaselineRelinkByteSize: baselineSize, ModifiedRelinkSHA256: modifiedDigest,
		ModifiedRelinkByteSize: modifiedSize, ModifiedFriBidiSourceSHA256: modifiedFriBidiSource,
		Smoke: observation, Files: files,
	}, nil
}

func rendererSourceGraph(
	ctx context.Context,
	repositoryRoot, dependencyRoot, harfBuzzRoot string,
	stdout, stderr io.Writer,
) ([]rendererGoPackage, []rendererGoModule, error) {
	goTool, err := tool.Resolve("go")
	if err != nil {
		return nil, nil, err
	}
	cflags := []string{
		"-I" + filepath.Join(dependencyRoot, "include", "freetype2"),
		"-I" + filepath.Join(dependencyRoot, "include", "fribidi"),
		"-I" + filepath.Join(harfBuzzRoot, "src"),
	}
	var output rendererBoundedBuffer
	output.limit = maximumRendererListBytes
	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: goTool,
		Args: []string{
			"list", "-deps", "-json", "-mod=readonly", "-tags", RendererBuildTag, RendererBuildPackage,
		},
		Directory: repositoryRoot, Env: rendererBuildEnvironment(cflags, []string{"-L" + filepath.Join(dependencyRoot, "lib")}),
		Stdout: &output, Stderr: stderr, Profile: lifecycle.ProfileDevelopment,
		Presentation: lifecycle.PresentationHeadless,
	}); err != nil || output.exceeded {
		return nil, nil, fmt.Errorf("inspect renderer source graph: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	packages := make([]rendererGoPackage, 0)
	moduleByPath := make(map[string]rendererGoModule)
	for {
		var current rendererGoPackage
		err := decoder.Decode(&current)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("decode renderer source graph: %w", err)
		}
		if current.Standard {
			continue
		}
		if current.Module == nil || current.Module.Replace != nil || current.Dir == "" || current.ImportPath == "" {
			return nil, nil, fmt.Errorf("renderer source graph contains an unsupported package")
		}
		packages = append(packages, current)
		if !current.Module.Main {
			moduleByPath[current.Module.Path] = *current.Module
		}
	}
	if len(packages) == 0 {
		return nil, nil, fmt.Errorf("renderer source graph is empty")
	}
	slices.SortFunc(packages, func(left, right rendererGoPackage) int {
		return strings.Compare(left.ImportPath, right.ImportPath)
	})
	modules := make([]rendererGoModule, 0, len(moduleByPath))
	for _, current := range moduleByPath {
		if current.Path == "" || current.Version == "" || current.Sum == "" || current.GoModSum == "" {
			return nil, nil, fmt.Errorf("renderer module closure is incomplete")
		}
		modules = append(modules, current)
	}
	slices.SortFunc(modules, func(left, right rendererGoModule) int {
		return strings.Compare(left.Path, right.Path)
	})
	_ = stdout
	return packages, modules, nil
}

func stageRendererModuleSource(
	repositoryRoot, destination string,
	packages []rendererGoPackage,
	modules []rendererGoModule,
) error {
	seen := make(map[string]struct{})
	for _, current := range packages {
		if current.Module == nil || !current.Module.Main {
			continue
		}
		for _, name := range rendererPackageFiles(current) {
			source := filepath.Join(current.Dir, name)
			relative, err := filepath.Rel(repositoryRoot, source)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) ||
				filepath.IsAbs(relative) {
				return fmt.Errorf("renderer source escapes its module")
			}
			relative = filepath.Clean(relative)
			if _, duplicate := seen[relative]; duplicate {
				continue
			}
			seen[relative] = struct{}{}
			if err := copyRegularFile(source, filepath.Join(destination, relative), 0o600); err != nil {
				return fmt.Errorf("stage renderer source %s: %w", relative, err)
			}
		}
	}
	if len(seen) == 0 {
		return fmt.Errorf("renderer local source closure is empty")
	}
	var goMod strings.Builder
	goMod.WriteString("module github.com/PerishCode/open-cut\n\ngo 1.25.0\n")
	if len(modules) != 0 {
		goMod.WriteString("\nrequire (\n")
		for _, module := range modules {
			fmt.Fprintf(&goMod, "\t%s %s\n", module.Path, module.Version)
		}
		goMod.WriteString(")\n")
	}
	if err := atomicfile.Write(filepath.Join(destination, "go.mod"), []byte(goMod.String()), 0o600); err != nil {
		return err
	}
	var goSum strings.Builder
	for _, module := range modules {
		fmt.Fprintf(&goSum, "%s %s %s\n", module.Path, module.Version, module.Sum)
		fmt.Fprintf(&goSum, "%s %s/go.mod %s\n", module.Path, module.Version, module.GoModSum)
	}
	return atomicfile.Write(filepath.Join(destination, "go.sum"), []byte(goSum.String()), 0o600)
}

func rendererPackageFiles(current rendererGoPackage) []string {
	result := make([]string, 0, len(current.GoFiles)+len(current.CgoFiles)+len(current.CFiles)+
		len(current.CXXFiles)+len(current.HFiles)+len(current.SFiles)+len(current.EmbedFiles))
	for _, source := range [][]string{
		current.GoFiles, current.CgoFiles, current.CFiles, current.CXXFiles,
		current.HFiles, current.SFiles, current.EmbedFiles,
	} {
		result = append(result, source...)
	}
	slices.Sort(result)
	return slices.Compact(result)
}

func stageRendererNativeInputs(dependencyRoot, harfBuzzRoot, destination string) error {
	for _, definition := range []struct{ source, relative string }{
		{filepath.Join(dependencyRoot, "include", "freetype2"), "include/freetype2"},
		{filepath.Join(dependencyRoot, "include", "fribidi"), "include/fribidi"},
	} {
		if err := copyRendererTree(definition.source, filepath.Join(destination, filepath.FromSlash(definition.relative)), nil); err != nil {
			return err
		}
	}
	if err := copyRendererTree(
		filepath.Join(harfBuzzRoot, "src"), filepath.Join(destination, "include", "harfbuzz"),
		func(relative string) bool { return strings.HasSuffix(relative, ".h") },
	); err != nil {
		return err
	}
	for _, name := range []string{"libfreetype.a", "libfribidi.a", "libharfbuzz.a"} {
		if err := copyRegularFile(
			filepath.Join(dependencyRoot, "lib", name), filepath.Join(destination, "lib", name), 0o600,
		); err != nil {
			return err
		}
	}
	return nil
}

func stageRendererNativeSources(archives map[string]string, destination string) error {
	if len(archives) != len(nativeTextSourceRecords()) {
		return fmt.Errorf("renderer corresponding-source set is invalid")
	}
	for _, source := range nativeTextSourceRecords() {
		archive, exists := archives[source.ID]
		if !exists {
			return fmt.Errorf("renderer corresponding source %s is unavailable", source.ID)
		}
		digest, _, err := digestFile(archive)
		if err != nil || digest != source.SHA256 {
			return fmt.Errorf("renderer corresponding source %s is invalid", source.ID)
		}
		suffix, err := sourceArchiveSuffix(source.URL)
		if err != nil {
			return err
		}
		if err := copyRegularFile(
			archive,
			filepath.Join(destination, "source", source.ID+"-"+source.Version+suffix),
			0o600,
		); err != nil {
			return err
		}
	}
	return nil
}

func copyRendererTree(sourceRoot, destinationRoot string, include func(string) bool) error {
	physical, err := filepath.EvalSymlinks(sourceRoot)
	if err != nil || filepath.Clean(physical) != filepath.Clean(sourceRoot) {
		return fmt.Errorf("renderer relink source tree is linked")
	}
	return filepath.WalkDir(sourceRoot, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filename == sourceRoot {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("renderer relink source contains a linked entry")
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("renderer relink source contains a non-regular entry")
		}
		relative, err := filepath.Rel(sourceRoot, filename)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if include != nil && !include(relative) {
			return nil
		}
		return copyRegularFile(filename, filepath.Join(destinationRoot, filepath.FromSlash(relative)), 0o600)
	})
}

func rendererKitNativeFlags(sourceRoot, nativeRoot string) ([]string, []string) {
	return []string{
		"-I" + filepath.Join(nativeRoot, "include", "freetype2"),
		"-I" + filepath.Join(nativeRoot, "include", "fribidi"),
		"-I" + filepath.Join(nativeRoot, "include", "harfbuzz"),
		"-ffile-prefix-map=" + sourceRoot + "=.",
	}, []string{"-L" + filepath.Join(nativeRoot, "lib")}
}

func buildRendererFromRelinkKit(
	ctx context.Context,
	goTool, sourceRoot, nativeRoot, output string,
	stdout, stderr io.Writer,
) error {
	cflags, ldflags := rendererKitNativeFlags(sourceRoot, nativeRoot)
	cacheRoot := filepath.Join(filepath.Dir(nativeRoot), ".gocache")
	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		return fmt.Errorf("create isolated renderer relink cache: %w", err)
	}
	buildEnvironment := environment.Merge(
		rendererBuildEnvironment(cflags, ldflags), nil, map[string]string{"GOCACHE": cacheRoot},
	)
	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: goTool,
		Args: []string{
			"build", "-buildvcs=false", "-trimpath", "-mod=vendor", "-tags", RendererBuildTag,
			"-ldflags=-buildid=", "-o", output, RendererBuildPackage,
		},
		Directory: sourceRoot, Env: buildEnvironment,
		Stdout: stdout, Stderr: stderr, Profile: lifecycle.ProfileDevelopment,
		Presentation: lifecycle.PresentationHeadless,
	}); err != nil {
		return fmt.Errorf("build renderer from relink kit: %w", err)
	}
	return verifyRendererDynamicClosure(output)
}

func rendererKitSourceClosure(root string) ([]RendererSourceFile, string, error) {
	files := make([]RendererSourceFile, 0)
	var total uint64
	err := filepath.WalkDir(root, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filename == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("renderer source kit contains a linked entry")
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() < 0 ||
			uint64(info.Size()) > maximumRendererKitBytes-total {
			return fmt.Errorf("renderer source kit exceeds its bound")
		}
		digest, size, err := digestRendererKitFile(filename)
		if err != nil {
			return err
		}
		total += size
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		files = append(files, RendererSourceFile{
			Path: filepath.ToSlash(relative), SHA256: digest, ByteSize: size,
		})
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	slices.SortFunc(files, func(left, right RendererSourceFile) int {
		return strings.Compare(left.Path, right.Path)
	})
	digest, err := closureDigest(rendererSourceClosureDomain, files)
	return files, digest, err
}

func digestRendererKitFile(filename string) (string, uint64, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", 0, err
	}
	digest := sha256.New()
	written, err := io.Copy(digest, file)
	if err != nil || written != info.Size() {
		return "", 0, fmt.Errorf("digest renderer kit file: %w", err)
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), uint64(written), nil
}

type rendererBoundedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (buffer *rendererBoundedBuffer) Write(value []byte) (int, error) {
	if buffer.exceeded {
		return len(value), nil
	}
	remaining := buffer.limit - buffer.Len()
	if len(value) > remaining {
		buffer.exceeded = true
		if remaining > 0 {
			_, _ = buffer.Buffer.Write(value[:remaining])
		}
		return len(value), nil
	}
	return buffer.Buffer.Write(value)
}
