package cbuild

// libvpx and libopus are built here rather than beside the manifest assembly
// they used to share a file with. Both are inputs to the C toolchain and
// nothing else; keeping them next to staging and publication meant an edit to
// either concern invalidated the other.

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/utils/environment"
	"github.com/PerishCode/open-cut/utils/target"
)

func normalizeBuildConfiguration(
	configuration []string,
	buildRoot string,
	dependencyRoot string,
	compiler string,
) []string {
	result := make([]string, len(configuration))
	replacements := []struct{ actual, token string }{
		{dependencyRoot, "$deps"},
		{shellBuildPath(dependencyRoot), "$deps"},
		{buildRoot, "$build"},
		{shellBuildPath(buildRoot), "$build"},
		{compiler, "$cc"},
		{shellBuildPath(compiler), "$cc"},
	}
	for index, value := range configuration {
		for _, replacement := range replacements {
			value = strings.ReplaceAll(value, replacement.actual, replacement.token)
		}
		result[index] = value
	}
	return result
}

func buildLibVPX(
	ctx context.Context,
	sourceRoot, prefix, compiler, shell, makeTool string,
	parallelism int,
	buildTarget target.Target,
	stdout, stderr io.Writer,
) ([]string, error) {
	configuration, err := libVPXConfiguration(shellBuildPath(sourceRoot), shellBuildPath(prefix), buildTarget)
	if err != nil {
		return nil, err
	}
	buildEnvironment := environment.Merge(os.Environ(), nil, map[string]string{"CC": shellBuildPath(compiler)})
	if err := runConfigure(
		ctx, shell, filepath.Join(sourceRoot, "configure"), configuration,
		sourceRoot, buildEnvironment, stdout, stderr,
	); err != nil {
		return nil, fmt.Errorf("configure pinned libvpx: %w", err)
	}
	for _, arguments := range [][]string{{"-j", fmt.Sprint(parallelism)}, {"install"}} {
		if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
			Executable: makeTool, Args: arguments, Directory: sourceRoot,
			Env: buildEnvironment, Stdout: stdout, Stderr: stderr,
			Profile: lifecycle.ProfileDevelopment, Presentation: lifecycle.PresentationHeadless,
		}); err != nil {
			return nil, fmt.Errorf("build pinned libvpx: %w", err)
		}
	}
	return configuration, nil
}

func libVPXTarget(buildTarget target.Target) (string, error) {
	switch buildTarget {
	case target.Target{Platform: target.Mac, Arch: target.ARM64}:
		return "arm64-darwin23-gcc", nil
	case target.Target{Platform: target.Mac, Arch: target.X64}:
		return "x86_64-darwin17-gcc", nil
	case target.Target{Platform: target.Linux, Arch: target.ARM64}:
		return "arm64-linux-gcc", nil
	case target.Target{Platform: target.Linux, Arch: target.X64}:
		return "x86_64-linux-gcc", nil
	case target.Target{Platform: target.Win, Arch: target.ARM64}:
		return "arm64-win64-gcc", nil
	case target.Target{Platform: target.Win, Arch: target.X64}:
		return "x86_64-win64-gcc", nil
	default:
		return "", fmt.Errorf("libvpx target is unsupported")
	}
}

func buildOpus(
	ctx context.Context,
	sourceRoot, prefix, compiler, shell, makeTool string,
	parallelism int,
	stdout, stderr io.Writer,
) ([]string, error) {
	configuration := opusConfiguration(shellBuildPath(sourceRoot), shellBuildPath(prefix))
	buildEnvironment := environment.Merge(os.Environ(), nil, map[string]string{
		"CC":     shellBuildPath(compiler),
		"CFLAGS": "-O2 -ffile-prefix-map=" + shellBuildPath(sourceRoot) + "=.",
	})
	if err := runConfigure(
		ctx, shell, filepath.Join(sourceRoot, "configure"), configuration,
		sourceRoot, buildEnvironment, stdout, stderr,
	); err != nil {
		return nil, fmt.Errorf("configure pinned libopus: %w", err)
	}
	for _, arguments := range [][]string{{"-j", fmt.Sprint(parallelism)}, {"install"}} {
		if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
			Executable: makeTool, Args: arguments, Directory: sourceRoot,
			Env: buildEnvironment, Stdout: stdout, Stderr: stderr,
			Profile: lifecycle.ProfileDevelopment, Presentation: lifecycle.PresentationHeadless,
		}); err != nil {
			return nil, fmt.Errorf("build pinned libopus: %w", err)
		}
	}
	return configuration, nil
}

func opusConfiguration(sourceRoot, prefix string) []string {
	return []string{
		"--prefix=" + prefix, "--disable-shared", "--enable-static", "--disable-doc",
		"--disable-extra-programs", "--enable-fixed-point", "--disable-asm", "--disable-rtcd", "--disable-intrinsics",
		"--disable-dependency-tracking",
	}
}
