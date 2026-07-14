package devbootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/PerishCode/open-cut/internal/sourcefingerprint"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/utils/target"
	"github.com/PerishCode/open-cut/utils/tool"
)

const ResultSchema = 1

var exactVersionPattern = regexp.MustCompile(`^(?:v)?([0-9]+)\.([0-9]+)\.([0-9]+)$`)

type Options struct {
	RepositoryRoot string
	Stdout         io.Writer
	Stderr         io.Writer
}

type ToolResult struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

type PackageManagerResult struct {
	ToolResult
	Managed bool `json:"managed"`
}

type ControlResult struct {
	Path              string `json:"path"`
	SourceFingerprint string `json:"sourceFingerprint"`
}

type Result struct {
	Schema       int                  `json:"schema"`
	Repository   string               `json:"repository"`
	Node         ToolResult           `json:"node"`
	Pnpm         PackageManagerResult `json:"pnpm"`
	Control      ControlResult        `json:"control"`
	HooksPath    string               `json:"hooksPath"`
	Dependencies string               `json:"dependencies"`
}

type manifest struct {
	PackageManager string `json:"packageManager"`
	Engines        struct {
		Node string `json:"node"`
	} `json:"engines"`
}

func Run(ctx context.Context, options Options) (Result, error) {
	root, requirements, err := loadRepository(options.RepositoryRoot)
	if err != nil {
		return Result{}, err
	}
	stdout, stderr := options.Stdout, options.Stderr
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	nodePath, err := tool.Resolve("node")
	if err != nil {
		return Result{}, fmt.Errorf("Node is a cold-start prerequisite: %w", err)
	}
	nodeCommand := tool.Command{Executable: nodePath}
	nodeInfo, err := tool.InspectCommand(ctx, "node", nodeCommand)
	if err != nil {
		return Result{}, err
	}
	if !satisfiesNodeRequirement(nodeInfo.Version, requirements.NodeRequirement) {
		return Result{}, fmt.Errorf("Node %s does not satisfy package.json engines.node %q", nodeInfo.Version, requirements.NodeRequirement)
	}

	pnpmCommand, pnpmManaged, err := ensurePnpm(ctx, root, nodeCommand, requirements.PnpmVersion, stdout, stderr)
	if err != nil {
		return Result{}, err
	}
	pnpmInfo, err := tool.InspectCommand(ctx, "pnpm", pnpmCommand)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimPrefix(pnpmInfo.Version, "v") != requirements.PnpmVersion {
		return Result{}, fmt.Errorf("pnpm %s does not match packageManager pnpm@%s", pnpmInfo.Version, requirements.PnpmVersion)
	}

	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: pnpmCommand.Executable,
		Args:       pnpmCommand.Arguments("install", "--frozen-lockfile"),
		Directory:  root,
		Stdout:     stdout,
		Stderr:     stderr,
		Profile:    lifecycle.ProfileDevelopment,
	}); err != nil {
		return Result{}, fmt.Errorf("install workspace dependencies: %w", err)
	}
	controlCommand, fingerprint, err := buildRepositoryControl(ctx, root, stdout, stderr)
	if err != nil {
		return Result{}, err
	}
	if err := tool.WriteRepositoryShims(root, map[string]tool.Command{
		"oc-control": controlCommand,
		"pnpm":       pnpmCommand,
	}); err != nil {
		return Result{}, err
	}
	repositoryPnpmCommand, err := tool.RepositoryShimCommand(root, "pnpm")
	if err != nil {
		return Result{}, err
	}
	repositoryControlCommand, err := tool.RepositoryShimCommand(root, "oc-control")
	if err != nil {
		return Result{}, err
	}
	if err := configureHooks(ctx, root); err != nil {
		return Result{}, err
	}
	if err := tool.SaveRepositoryState(root, tool.RepositoryState{
		Schema: tool.RepositoryStateSchema,
		Tools: map[string]tool.Command{
			"oc-control": repositoryControlCommand,
			"pnpm":       repositoryPnpmCommand,
		},
	}); err != nil {
		return Result{}, err
	}

	return Result{
		Schema:     ResultSchema,
		Repository: root,
		Node:       ToolResult{Path: nodeInfo.Path, Version: nodeInfo.Version},
		Pnpm: PackageManagerResult{
			ToolResult: ToolResult{Path: pnpmInfo.Path, Version: pnpmInfo.Version},
			Managed:    pnpmManaged,
		},
		Control:      ControlResult{Path: repositoryControlCommand.Executable, SourceFingerprint: fingerprint},
		HooksPath:    ".githooks",
		Dependencies: "installed",
	}, nil
}

func buildRepositoryControl(
	ctx context.Context,
	root string,
	stdout, stderr io.Writer,
) (tool.Command, string, error) {
	fingerprint, err := sourcefingerprint.Calculate(root)
	if err != nil {
		return tool.Command{}, "", err
	}
	toolRoot := filepath.Join(root, ".oc-control", "tools", "oc-control", fingerprint)
	artifact := filepath.Join(toolRoot, target.Host().ExecutableName("oc-control"))
	if info, statErr := os.Stat(artifact); statErr == nil && info.Mode().IsRegular() {
		return tool.Command{Executable: artifact}, fingerprint, nil
	}
	goPath, err := tool.Resolve("go")
	if err != nil {
		return tool.Command{}, "", fmt.Errorf("Go is a cold-start prerequisite: %w", err)
	}
	parent := filepath.Dir(toolRoot)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return tool.Command{}, "", fmt.Errorf("create control tool root: %w", err)
	}
	staging, err := os.MkdirTemp(parent, ".oc-control-bootstrap-")
	if err != nil {
		return tool.Command{}, "", fmt.Errorf("create control tool staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	stagedArtifact := filepath.Join(staging, target.Host().ExecutableName("oc-control"))
	linkerValue := "github.com/PerishCode/open-cut/internal/buildinfo.DevelopmentSourceFingerprint=" + fingerprint
	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: goPath,
		Args:       []string{"build", "-trimpath", "-ldflags", "-X " + linkerValue, "-o", stagedArtifact, "./cmd/oc-control"},
		Directory:  root,
		Stdout:     stdout,
		Stderr:     stderr,
		Profile:    lifecycle.ProfileDevelopment,
	}); err != nil {
		return tool.Command{}, "", fmt.Errorf("build checkout-pinned oc-control: %w", err)
	}
	if err := os.Rename(staging, toolRoot); err != nil {
		return tool.Command{}, "", fmt.Errorf("activate checkout-pinned oc-control: %w", err)
	}
	return tool.Command{Executable: artifact}, fingerprint, nil
}

type requirements struct {
	NodeRequirement string
	PnpmVersion     string
}

func loadRepository(repositoryRoot string) (string, requirements, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return "", requirements{}, fmt.Errorf("resolve repository root: %w", err)
	}
	for _, marker := range []string{".git", "go.mod", "pnpm-workspace.yaml", "oc-control.json"} {
		if _, err := os.Stat(filepath.Join(root, marker)); err != nil {
			return "", requirements{}, fmt.Errorf("repository marker %s: %w", marker, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return "", requirements{}, fmt.Errorf("read package.json: %w", err)
	}
	var packageManifest manifest
	if err := json.Unmarshal(data, &packageManifest); err != nil {
		return "", requirements{}, fmt.Errorf("decode package.json: %w", err)
	}
	const prefix = "pnpm@"
	if !strings.HasPrefix(packageManifest.PackageManager, prefix) {
		return "", requirements{}, fmt.Errorf("package.json packageManager must pin pnpm@<version>")
	}
	pnpmVersion := strings.TrimPrefix(packageManifest.PackageManager, prefix)
	if !exactVersionPattern.MatchString(pnpmVersion) {
		return "", requirements{}, fmt.Errorf("package.json packageManager must pin an exact pnpm version")
	}
	if packageManifest.Engines.Node == "" {
		return "", requirements{}, fmt.Errorf("package.json engines.node is required")
	}
	return root, requirements{NodeRequirement: packageManifest.Engines.Node, PnpmVersion: pnpmVersion}, nil
}

func satisfiesNodeRequirement(actual, requirement string) bool {
	match := exactVersionPattern.FindStringSubmatch(strings.TrimSpace(actual))
	if match == nil {
		return false
	}
	if strings.HasPrefix(requirement, "~") {
		major, err := strconv.Atoi(strings.TrimPrefix(requirement, "~"))
		return err == nil && match[1] == strconv.Itoa(major)
	}
	required := exactVersionPattern.FindStringSubmatch(requirement)
	return required != nil && match[1] == required[1] && match[2] == required[2] && match[3] == required[3]
}

func ensurePnpm(
	ctx context.Context,
	root string,
	nodeCommand tool.Command,
	version string,
	stdout, stderr io.Writer,
) (tool.Command, bool, error) {
	if pnpmPath, err := tool.Resolve("pnpm"); err == nil {
		candidate := tool.Command{Executable: pnpmPath}
		if info, inspectErr := tool.InspectCommand(ctx, "pnpm", candidate); inspectErr == nil && strings.TrimPrefix(info.Version, "v") == version {
			return candidate, false, nil
		}
	}

	destination := filepath.Join(root, ".oc-control", "tools", "pnpm-"+version)
	pnpmEntry := filepath.Join(destination, "node_modules", "pnpm", "bin", "pnpm.cjs")
	if info, err := os.Stat(pnpmEntry); err == nil && info.Mode().IsRegular() {
		candidate := tool.Command{Executable: nodeCommand.Executable, Prefix: []string{pnpmEntry}}
		if inspected, inspectErr := tool.InspectCommand(ctx, "pnpm", candidate); inspectErr == nil && strings.TrimPrefix(inspected.Version, "v") == version {
			return candidate, true, nil
		}
	}
	if err := os.RemoveAll(destination); err != nil {
		return tool.Command{}, false, fmt.Errorf("remove incomplete pnpm tool: %w", err)
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return tool.Command{}, false, fmt.Errorf("create pnpm tool root: %w", err)
	}
	staging, err := os.MkdirTemp(parent, ".pnpm-bootstrap-")
	if err != nil {
		return tool.Command{}, false, fmt.Errorf("create pnpm staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	npmPath, err := tool.Resolve("npm")
	if err != nil {
		return tool.Command{}, false, fmt.Errorf("npm is required to provision pinned pnpm: %w", err)
	}
	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: npmPath,
		Args: []string{
			"install", "--prefix", staging, "--ignore-scripts", "--no-save", "--package-lock=false",
			"--no-audit", "--no-fund", "pnpm@" + version,
		},
		Directory: root,
		Stdout:    stdout,
		Stderr:    stderr,
		Profile:   lifecycle.ProfileDevelopment,
	}); err != nil {
		return tool.Command{}, false, fmt.Errorf("provision pnpm %s: %w", version, err)
	}
	stagedEntry := filepath.Join(staging, "node_modules", "pnpm", "bin", "pnpm.cjs")
	if info, err := os.Stat(stagedEntry); err != nil || !info.Mode().IsRegular() {
		return tool.Command{}, false, fmt.Errorf("provisioned pnpm entry is missing at %s", stagedEntry)
	}
	if err := os.Rename(staging, destination); err != nil {
		return tool.Command{}, false, fmt.Errorf("activate pnpm tool: %w", err)
	}
	return tool.Command{Executable: nodeCommand.Executable, Prefix: []string{pnpmEntry}}, true, nil
}

func configureHooks(ctx context.Context, root string) error {
	hook := filepath.Join(root, ".githooks", "pre-commit")
	if info, err := os.Stat(hook); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("pre-commit hook is missing at %s", hook)
	}
	if err := os.Chmod(hook, 0o755); err != nil {
		return fmt.Errorf("make pre-commit hook executable: %w", err)
	}
	gitPath, err := tool.Resolve("git")
	if err != nil {
		return err
	}
	var output bytes.Buffer
	read := exec.CommandContext(ctx, gitPath, "config", "--local", "--get", "core.hooksPath")
	read.Dir = root
	read.Stdout, read.Stderr = &output, &output
	readErr := read.Run()
	current := strings.TrimSpace(output.String())
	if readErr != nil {
		var exitError *exec.ExitError
		if !errors.As(readErr, &exitError) || exitError.ExitCode() != 1 {
			return fmt.Errorf("read core.hooksPath: %w: %s", readErr, current)
		}
	}
	if current != "" && current != ".githooks" {
		return fmt.Errorf("core.hooksPath is already %q; refusing to replace it", current)
	}
	if current == ".githooks" {
		return nil
	}
	write := exec.CommandContext(ctx, gitPath, "config", "--local", "core.hooksPath", ".githooks")
	write.Dir = root
	write.Stdout, write.Stderr = &output, &output
	if err := write.Run(); err != nil {
		return fmt.Errorf("set core.hooksPath: %w: %s", err, strings.TrimSpace(output.String()))
	}
	return nil
}
