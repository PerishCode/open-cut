package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/PerishCode/open-cut/utils/atomicfile"
)

const RepositoryStateSchema = 1

type Command struct {
	Executable string   `json:"executable"`
	Prefix     []string `json:"prefix,omitempty"`
}

func (command Command) Arguments(args ...string) []string {
	return append(append([]string{}, command.Prefix...), args...)
}

type RepositoryState struct {
	Schema int                `json:"schema"`
	Tools  map[string]Command `json:"tools"`
}

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
	path, err = filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute tool path %s: %w", name, err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve physical tool path %s: %w", name, err)
	}
	return path, nil
}

func ResolveRepository(repositoryRoot, name string) (Command, error) {
	state, err := LoadRepositoryState(repositoryRoot)
	if err == nil {
		if command, ok := state.Tools[name]; ok {
			if err := validateCommand(name, command); err != nil {
				return Command{}, err
			}
			return command, nil
		}
	} else if !os.IsNotExist(err) {
		return Command{}, err
	}
	path, resolveErr := Resolve(name)
	if resolveErr != nil {
		return Command{}, resolveErr
	}
	return Command{Executable: path}, nil
}

func RepositoryStatePath(repositoryRoot string) string {
	return filepath.Join(repositoryRoot, ".oc-control", "toolchain.json")
}

func LoadRepositoryState(repositoryRoot string) (RepositoryState, error) {
	data, err := os.ReadFile(RepositoryStatePath(repositoryRoot))
	if err != nil {
		return RepositoryState{}, err
	}
	var state RepositoryState
	if err := json.Unmarshal(data, &state); err != nil {
		return RepositoryState{}, fmt.Errorf("decode repository toolchain: %w", err)
	}
	if state.Schema != RepositoryStateSchema || state.Tools == nil {
		return RepositoryState{}, fmt.Errorf("repository toolchain requires schema %d and tools", RepositoryStateSchema)
	}
	return state, nil
}

func SaveRepositoryState(repositoryRoot string, state RepositoryState) error {
	if state.Schema != RepositoryStateSchema || state.Tools == nil {
		return fmt.Errorf("repository toolchain requires schema %d and tools", RepositoryStateSchema)
	}
	for name, command := range state.Tools {
		if err := validateCommand(name, command); err != nil {
			return err
		}
	}
	return atomicfile.WriteJSON(RepositoryStatePath(repositoryRoot), state, 0o600)
}

// WriteRepositoryShims materializes both supported shell entry forms so callers
// never need to model host-specific executable wrapper syntax.
func WriteRepositoryShims(repositoryRoot string, commands map[string]Command) error {
	binRoot := filepath.Join(repositoryRoot, ".oc-control", "bin")
	for name, command := range commands {
		if err := validateCommand(name, command); err != nil {
			return err
		}
		arguments := append([]string{command.Executable}, command.Prefix...)
		posix := "#!/bin/sh\nexec " + shellJoin(arguments) + " \"$@\"\n"
		if err := atomicfile.Write(filepath.Join(binRoot, name), []byte(posix), 0o755); err != nil {
			return fmt.Errorf("write %s shim: %w", name, err)
		}
		windows := "@echo off\r\n" + windowsJoin(arguments) + " %*\r\n"
		if err := atomicfile.Write(filepath.Join(binRoot, name+".cmd"), []byte(windows), 0o644); err != nil {
			return fmt.Errorf("write %s Windows shim: %w", name, err)
		}
	}
	return nil
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

func InspectCommand(ctx context.Context, name string, command Command) (Info, error) {
	if err := validateCommand(name, command); err != nil {
		return Info{Name: name}, err
	}
	process := exec.CommandContext(ctx, command.Executable, command.Arguments(versionArguments(name)...)...)
	var output bytes.Buffer
	process.Stdout, process.Stderr = &output, &output
	if err := process.Run(); err != nil {
		return Info{Name: name, Path: command.Executable}, fmt.Errorf("inspect tool %s: %w", name, err)
	}
	return Info{Name: name, Path: command.Executable, Version: strings.TrimSpace(output.String())}, nil
}

func validateCommand(name string, command Command) error {
	if command.Executable == "" || !filepath.IsAbs(command.Executable) {
		return fmt.Errorf("repository tool %s requires an absolute executable", name)
	}
	info, err := os.Stat(command.Executable)
	if err != nil {
		return fmt.Errorf("repository tool %s executable: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("repository tool %s executable is not a regular file", name)
	}
	return nil
}

func shellJoin(arguments []string) string {
	quoted := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		quoted = append(quoted, "'"+strings.ReplaceAll(argument, "'", "'\"'\"'")+"'")
	}
	return strings.Join(quoted, " ")
}

func windowsJoin(arguments []string) string {
	quoted := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		quoted = append(quoted, "\""+strings.ReplaceAll(argument, "\"", "\\\"")+"\"")
	}
	return strings.Join(quoted, " ")
}

func versionArguments(name string) []string {
	if name == "go" {
		return []string{"version"}
	}
	return []string{"--version"}
}
