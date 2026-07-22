package cbuild

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/utils/tool"
)

type buildTools struct {
	compiler string
	cxx      string
	archiver string
	makeTool string
	cmake    string
	shell    string
}

func resolveBuildTools(ctx context.Context) (buildTools, string, error) {
	var result buildTools
	for _, binding := range []struct {
		name        string
		destination *string
	}{
		{name: "cc", destination: &result.compiler},
		{name: "c++", destination: &result.cxx},
		{name: "ar", destination: &result.archiver},
		{name: "make", destination: &result.makeTool},
		{name: "cmake", destination: &result.cmake},
		{name: "sh", destination: &result.shell},
	} {
		resolved, err := tool.Resolve(binding.name)
		if err != nil {
			return buildTools{}, "", fmt.Errorf("resolve media build tool %s: %w", binding.name, err)
		}
		*binding.destination = resolved
	}
	identity, err := inspectBuildTools(
		ctx, result.compiler, result.cxx, result.archiver, result.makeTool, result.cmake,
	)
	if err != nil {
		return buildTools{}, "", err
	}
	return result, identity, nil
}

// BuildEnvironmentIdentity is the exact compiler/tool identity persisted in a
// C-tree stamp. Cache keys consume this same value so a restored generation
// cannot claim compatibility that EnsureTree would reject.
func BuildEnvironmentIdentity(ctx context.Context) (string, error) {
	_, identity, err := resolveBuildTools(ctx)
	return identity, err
}

func inspectCompiler(ctx context.Context, compiler string) (string, error) {
	command := exec.CommandContext(ctx, compiler, "--version")
	output, err := command.CombinedOutput()
	if err != nil || len(output) == 0 || len(output) > 16<<10 {
		return "", fmt.Errorf("inspect media compiler")
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	identity := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "InstalledDir:") {
			continue
		}
		identity = append(identity, line)
	}
	if len(identity) == 0 {
		return "", fmt.Errorf("inspect media compiler")
	}
	return strings.Join(identity, "\n"), nil
}

func inspectBuildTools(
	ctx context.Context,
	compiler, cxx, archiver, makeTool string,
	additional ...string,
) (string, error) {
	definitions := []struct{ name, executable string }{
		{"CC", compiler}, {"CXX", cxx}, {"MAKE", makeTool},
	}
	parts := make([]string, 0, len(definitions)+1)
	for _, definition := range definitions {
		identity, err := inspectCompiler(ctx, definition.executable)
		if err != nil {
			return "", fmt.Errorf("inspect %s for media build: %w", definition.name, err)
		}
		parts = append(parts, definition.name+":\n"+identity)
	}
	for index, executable := range additional {
		identity, err := inspectCompiler(ctx, executable)
		if err != nil {
			return "", fmt.Errorf("inspect additional media build tool %d: %w", index, err)
		}
		parts = append(parts, fmt.Sprintf("TOOL-%d:\n%s", index, identity))
	}
	archiverDigest, archiverSize, err := digestFile(archiver)
	if err != nil {
		return "", fmt.Errorf("inspect AR for media build: %w", err)
	}
	parts = append(parts, fmt.Sprintf("AR:\n%s bytes:%d", archiverDigest, archiverSize))
	return strings.Join(parts, "\n"), nil
}

func runConfigure(
	ctx context.Context,
	shell, script string,
	arguments []string,
	directory string,
	env []string,
	stdout, stderr io.Writer,
) error {
	return lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: shell, Args: append([]string{shellBuildPath(script)}, arguments...), Directory: directory, Env: env,
		Stdout: stdout, Stderr: stderr, Profile: lifecycle.ProfileDevelopment,
		Presentation: lifecycle.PresentationHeadless,
	})
}

func shellBuildPath(value string) string {
	return shellBuildPathForOS(runtime.GOOS, value)
}

func shellBuildPathForOS(goos, value string) string {
	if goos != "windows" {
		return value
	}
	normalized := strings.ReplaceAll(value, `\`, "/")
	if len(normalized) >= 3 && normalized[1] == ':' && normalized[2] == '/' &&
		((normalized[0] >= 'A' && normalized[0] <= 'Z') || (normalized[0] >= 'a' && normalized[0] <= 'z')) {
		return "/" + strings.ToLower(normalized[:1]) + normalized[2:]
	}
	return normalized
}

func RepositoryMarker(root string) bool {
	for _, name := range []string{"go.mod", "oc-control.json", "pnpm-workspace.yaml"} {
		if info, err := os.Stat(filepath.Join(root, name)); err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}
