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

	"github.com/PerishCode/open-cut/lifecycle"
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

type Result struct {
	Schema       int        `json:"schema"`
	Repository   string     `json:"repository"`
	Node         ToolResult `json:"node"`
	Pnpm         ToolResult `json:"pnpm"`
	HooksPath    string     `json:"hooksPath"`
	Dependencies string     `json:"dependencies"`
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

	nodeInfo, err := tool.Inspect(ctx, "node")
	if err != nil {
		return Result{}, fmt.Errorf("Node is a development prerequisite: %w", err)
	}
	if !satisfiesNodeRequirement(nodeInfo.Version, requirements.NodeRequirement) {
		return Result{}, fmt.Errorf("Node %s does not satisfy package.json engines.node %q", nodeInfo.Version, requirements.NodeRequirement)
	}

	pnpmInfo, err := tool.Inspect(ctx, "pnpm")
	if err != nil {
		return Result{}, fmt.Errorf("pnpm is a development prerequisite: %w", err)
	}
	if strings.TrimPrefix(pnpmInfo.Version, "v") != requirements.PnpmVersion {
		return Result{}, fmt.Errorf("pnpm %s does not match packageManager pnpm@%s", pnpmInfo.Version, requirements.PnpmVersion)
	}

	if err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: pnpmInfo.Path,
		Args:       []string{"install", "--frozen-lockfile"},
		Directory:  root,
		Stdout:     stdout,
		Stderr:     stderr,
		Profile:    lifecycle.ProfileDevelopment,
	}); err != nil {
		return Result{}, fmt.Errorf("install workspace dependencies: %w", err)
	}
	if err := configureHooks(ctx, root); err != nil {
		return Result{}, err
	}

	return Result{
		Schema:       ResultSchema,
		Repository:   root,
		Node:         ToolResult{Path: nodeInfo.Path, Version: nodeInfo.Version},
		Pnpm:         ToolResult{Path: pnpmInfo.Path, Version: pnpmInfo.Version},
		HooksPath:    ".githooks",
		Dependencies: "installed",
	}, nil
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
