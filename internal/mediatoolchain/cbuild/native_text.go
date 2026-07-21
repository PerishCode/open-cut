package cbuild

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/utils/environment"
)

const (
	HarfBuzzSourceVersion = "14.2.1"
	FreeTypeSourceVersion = "2.14.3"
	FriBidiSourceVersion  = "1.0.16"
)

type NativeTextBuildRecipe struct {
	FreeType []string `json:"freeType"`
	FriBidi  []string `json:"friBidi"`
	HarfBuzz []string `json:"harfBuzz"`
}

func NativeTextSourceRecords() []SourceRecord {
	return []SourceRecord{
		{
			ID: "freetype", Version: FreeTypeSourceVersion,
			URL:     "https://download.savannah.gnu.org/releases/freetype/freetype-2.14.3.tar.xz",
			SHA256:  "sha256:36bc4f1cc413335368ee656c42afca65c5a3987e8768cc28cf11ba775e785a5f",
			License: "FTL",
		},
		{
			ID: "fribidi", Version: FriBidiSourceVersion,
			URL:     "https://github.com/fribidi/fribidi/releases/download/v1.0.16/fribidi-1.0.16.tar.xz",
			SHA256:  "sha256:1b1cde5b235d40479e91be2f0e88a309e3214c8ab470ec8a2744d82a5a9ea05c",
			License: "LGPL-2.1-or-later",
		},
		{
			ID: "harfbuzz", Version: HarfBuzzSourceVersion,
			URL:     "https://github.com/harfbuzz/harfbuzz/releases/download/14.2.1/harfbuzz-14.2.1.tar.xz",
			SHA256:  "sha256:a54a5d8e9380a41fbb762ce367bcbf7704792dfca0d93f1bbca86c5a57902e0e",
			License: "MIT",
		},
	}
}

func StageNativeTextNotices(roots map[string]string, stageRoot string) ([]NoticeRecord, error) {
	definitions := []struct{ id, source, relative string }{
		{"freetype-license", filepath.Join(roots["freetype"], "LICENSE.TXT"), "licenses/media/FREETYPE-LICENSE.txt"},
		{"freetype-ftl", filepath.Join(roots["freetype"], "docs", "FTL.TXT"), "licenses/media/FREETYPE-FTL.txt"},
		{"fribidi-lgpl", filepath.Join(roots["fribidi"], "COPYING"), "licenses/media/FRIBIDI-LGPL.txt"},
		{"harfbuzz-license", filepath.Join(roots["harfbuzz"], "COPYING"), "licenses/media/HARFBUZZ-LICENSE.txt"},
	}
	result := make([]NoticeRecord, 0, len(definitions))
	for _, definition := range definitions {
		destination := filepath.Join(stageRoot, filepath.FromSlash(definition.relative))
		if err := copyRegularFile(definition.source, destination, 0o600); err != nil {
			return nil, err
		}
		digest, size, err := digestFile(destination)
		if err != nil {
			return nil, err
		}
		result = append(result, NoticeRecord{
			ID: definition.id, Path: definition.relative, SHA256: digest, ByteSize: size,
		})
	}
	return result, nil
}

func extractNativeTextSources(archives map[string]string, destination string) (map[string]string, error) {
	roots := make(map[string]string, 3)
	var err error
	roots["freetype"], err = extractSource(
		archives["freetype"], destination, "freetype-"+FreeTypeSourceVersion, "configure",
	)
	if err != nil {
		return nil, fmt.Errorf("extract pinned FreeType: %w", err)
	}
	roots["fribidi"], err = extractSource(
		archives["fribidi"], destination, "fribidi-"+FriBidiSourceVersion, "configure",
	)
	if err != nil {
		return nil, fmt.Errorf("extract pinned FriBidi: %w", err)
	}
	roots["harfbuzz"], err = extractSourceIgnoringLinks(
		archives["harfbuzz"], destination, "harfbuzz-"+HarfBuzzSourceVersion, "src/harfbuzz.cc",
		[]archiveIgnoredLink{{
			Member: "harfbuzz-" + HarfBuzzSourceVersion + "/CLAUDE.md", Target: "AGENTS.md",
		}},
	)
	if err != nil {
		return nil, fmt.Errorf("extract pinned HarfBuzz: %w", err)
	}
	return roots, nil
}

func buildStaticNativeTextDependencies(
	ctx context.Context,
	roots map[string]string,
	prefix, compiler, cxx, archiver, shell, makeTool string,
	parallelism int,
	stdout, stderr io.Writer,
) (NativeTextBuildRecipe, error) {
	if len(roots) != 3 || roots["freetype"] == "" || roots["fribidi"] == "" || roots["harfbuzz"] == "" ||
		prefix == "" || compiler == "" || cxx == "" || archiver == "" || makeTool == "" || parallelism < 1 {
		return NativeTextBuildRecipe{}, fmt.Errorf("native text build contract is invalid")
	}
	freeType, err := buildFreeType(
		ctx, roots["freetype"], prefix, compiler, shell, makeTool, parallelism, stdout, stderr,
	)
	if err != nil {
		return NativeTextBuildRecipe{}, err
	}
	friBidi, err := BuildFriBidi(
		ctx, roots["fribidi"], prefix, compiler, shell, makeTool, parallelism, stdout, stderr,
	)
	if err != nil {
		return NativeTextBuildRecipe{}, err
	}
	harfBuzz, err := buildHarfBuzz(
		ctx, roots["harfbuzz"], prefix, cxx, archiver, stdout, stderr,
	)
	if err != nil {
		return NativeTextBuildRecipe{}, err
	}
	return NativeTextBuildRecipe{
		FreeType: normalizeNativeTextConfiguration(freeType, roots, prefix, compiler, cxx, archiver, makeTool),
		FriBidi:  normalizeNativeTextConfiguration(friBidi, roots, prefix, compiler, cxx, archiver, makeTool),
		HarfBuzz: normalizeNativeTextConfiguration(harfBuzz, roots, prefix, compiler, cxx, archiver, makeTool),
	}, nil
}

func buildFreeType(
	ctx context.Context,
	sourceRoot, prefix, compiler, shell, makeTool string,
	parallelism int,
	stdout, stderr io.Writer,
) ([]string, error) {
	shellCompiler := shellBuildPath(compiler)
	shellSourceRoot := shellBuildPath(sourceRoot)
	configuration := []string{
		"--prefix=" + shellBuildPath(prefix), "--disable-shared", "--enable-static", "--enable-pic",
		"--disable-freetype-config", "--with-zlib=no", "--with-bzip2=no", "--with-png=no",
		"--with-harfbuzz=no", "--with-brotli=no", "--with-librsvg=no",
		"--without-old-mac-fonts", "--without-fsspec", "--without-fsref", "--without-quickdraw-toolbox",
		"--without-quickdraw-carbon", "--without-ats",
	}
	buildEnvironment := environment.Merge(os.Environ(), nil, map[string]string{
		"CC": shellCompiler, "CFLAGS": "-O2 -fPIC -ffile-prefix-map=" + shellSourceRoot + "=.",
	})
	if err := runConfigure(ctx, shell, filepath.Join(sourceRoot, "configure"), configuration,
		sourceRoot, buildEnvironment, stdout, stderr); err != nil {
		return nil, fmt.Errorf("configure pinned FreeType: %w", err)
	}
	if err := runNativeTextMake(ctx, sourceRoot, buildEnvironment, makeTool, parallelism, stdout, stderr); err != nil {
		return nil, fmt.Errorf("build pinned FreeType: %w", err)
	}
	if err := verifyNativeArchive(filepath.Join(prefix, "lib", "libfreetype.a")); err != nil {
		return nil, err
	}
	return configuration, nil
}

func BuildFriBidi(
	ctx context.Context,
	sourceRoot, prefix, compiler, shell, makeTool string,
	parallelism int,
	stdout, stderr io.Writer,
) ([]string, error) {
	shellCompiler := shellBuildPath(compiler)
	shellSourceRoot := shellBuildPath(sourceRoot)
	configuration := []string{
		"--prefix=" + shellBuildPath(prefix), "--disable-shared", "--enable-static", "--with-pic",
		"--disable-dependency-tracking", "--disable-debug", "--disable-deprecated",
	}
	buildEnvironment := environment.Merge(os.Environ(), nil, map[string]string{
		"CC": shellCompiler, "CFLAGS": "-O2 -fPIC -ffile-prefix-map=" + shellSourceRoot + "=.",
	})
	if err := runConfigure(ctx, shell, filepath.Join(sourceRoot, "configure"), configuration,
		sourceRoot, buildEnvironment, stdout, stderr); err != nil {
		return nil, fmt.Errorf("configure pinned FriBidi: %w", err)
	}
	for _, arguments := range [][]string{
		{"-C", "lib", "-j", fmt.Sprint(parallelism)}, {"-C", "lib", "install"},
	} {
		if err := runNativeTextProcess(
			ctx, makeTool, arguments, sourceRoot, buildEnvironment, stdout, stderr,
		); err != nil {
			return nil, fmt.Errorf("build pinned FriBidi: %w", err)
		}
	}
	if err := verifyNativeArchive(filepath.Join(prefix, "lib", "libfribidi.a")); err != nil {
		return nil, err
	}
	return configuration, nil
}

func buildHarfBuzz(
	ctx context.Context,
	sourceRoot, prefix, cxx, archiver string,
	stdout, stderr io.Writer,
) ([]string, error) {
	objectRoot := filepath.Join(prefix, "obj", "harfbuzz")
	if err := os.MkdirAll(objectRoot, 0o700); err != nil {
		return nil, err
	}
	object := filepath.Join(objectRoot, "harfbuzz.o")
	arguments := []string{
		"-std=c++11", "-O2", "-fPIC", "-DHAVE_FREETYPE=1",
		"-I" + filepath.Join(sourceRoot, "src"), "-I" + filepath.Join(prefix, "include", "freetype2"),
		"-ffile-prefix-map=" + sourceRoot + "=.", "-c", filepath.Join(sourceRoot, "src", "harfbuzz.cc"),
		"-o", object,
	}
	if err := runNativeTextProcess(ctx, cxx, arguments, sourceRoot, os.Environ(), stdout, stderr); err != nil {
		return nil, fmt.Errorf("compile pinned HarfBuzz: %w", err)
	}
	archive := filepath.Join(prefix, "lib", "libharfbuzz.a")
	if err := os.MkdirAll(filepath.Dir(archive), 0o700); err != nil {
		return nil, err
	}
	archiveArguments := []string{"rcs", archive, object}
	if err := runNativeTextProcess(ctx, archiver, archiveArguments, sourceRoot, os.Environ(), stdout, stderr); err != nil {
		return nil, fmt.Errorf("archive pinned HarfBuzz: %w", err)
	}
	if err := verifyNativeArchive(archive); err != nil {
		return nil, err
	}
	return append(arguments, append([]string{"$archive"}, archiveArguments...)...), nil
}

func runNativeTextMake(
	ctx context.Context,
	directory string,
	env []string,
	makeTool string,
	parallelism int,
	stdout, stderr io.Writer,
) error {
	for _, arguments := range [][]string{{"-j", fmt.Sprint(parallelism)}, {"install"}} {
		if err := runNativeTextProcess(ctx, makeTool, arguments, directory, env, stdout, stderr); err != nil {
			return err
		}
	}
	return nil
}

func runNativeTextProcess(
	ctx context.Context,
	executable string,
	arguments []string,
	directory string,
	env []string,
	stdout, stderr io.Writer,
) error {
	return lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: executable, Args: arguments, Directory: directory, Env: env,
		Stdout: stdout, Stderr: stderr, Profile: lifecycle.ProfileDevelopment,
		Presentation: lifecycle.PresentationHeadless,
	})
}

func verifyNativeArchive(filename string) error {
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 {
		return fmt.Errorf("native text build did not produce %s", filepath.Base(filename))
	}
	return nil
}

func normalizeNativeTextConfiguration(
	configuration []string,
	roots map[string]string,
	prefix, compiler, cxx, archiver, makeTool string,
) []string {
	replacements := []struct{ actual, token string }{
		{roots["freetype"], "$freetype"}, {roots["fribidi"], "$fribidi"},
		{roots["harfbuzz"], "$harfbuzz"}, {prefix, "$deps"},
		{compiler, "$cc"}, {cxx, "$cxx"}, {archiver, "$ar"}, {makeTool, "$make"},
		{shellBuildPath(roots["freetype"]), "$freetype"},
		{shellBuildPath(roots["fribidi"]), "$fribidi"},
		{shellBuildPath(roots["harfbuzz"]), "$harfbuzz"},
		{shellBuildPath(prefix), "$deps"},
		{shellBuildPath(compiler), "$cc"}, {shellBuildPath(cxx), "$cxx"},
		{shellBuildPath(archiver), "$ar"}, {shellBuildPath(makeTool), "$make"},
	}
	result := make([]string, len(configuration))
	for index, value := range configuration {
		for _, replacement := range replacements {
			value = strings.ReplaceAll(value, replacement.actual, replacement.token)
		}
		result[index] = value
	}
	return result
}
