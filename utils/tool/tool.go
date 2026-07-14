package tool

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Info struct {
	Name    string
	Path    string
	Version string
}

func Resolve(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("resolve tool %s: %w", name, err)
	}
	return path, nil
}

func Inspect(ctx context.Context, name string) (Info, error) {
	path, err := Resolve(name)
	if err != nil {
		return Info{Name: name}, err
	}
	command := exec.CommandContext(ctx, path, versionArguments(name)...)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Run(); err != nil {
		return Info{Name: name, Path: path}, fmt.Errorf("inspect tool %s: %w", name, err)
	}
	return Info{Name: name, Path: path, Version: strings.TrimSpace(output.String())}, nil
}

func versionArguments(name string) []string {
	if name == "go" {
		return []string{"version"}
	}
	return []string{"--version"}
}
