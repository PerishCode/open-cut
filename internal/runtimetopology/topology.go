package runtimetopology

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/sidecar/protocol"
	"github.com/PerishCode/open-cut/utils/atomicfile"
)

const Schema = 1

var (
	appPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)
	envPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Process is a platform-resolved, product-agnostic command descriptor. Paths
// are slash-separated and relative to the directory containing the topology.
type Process struct {
	App              string                  `json:"app"`
	Command          string                  `json:"command"`
	Args             []string                `json:"args,omitempty"`
	WorkingDirectory string                  `json:"workingDirectory"`
	Env              map[string]string       `json:"env,omitempty"`
	UnsetEnv         []string                `json:"unsetEnv,omitempty"`
	Capabilities     []protocol.Capability   `json:"capabilities,omitempty"`
	Sandbox          lifecycle.SandboxPolicy `json:"sandbox,omitempty"`
}

type Topology struct {
	Schema    int       `json:"schema"`
	Processes []Process `json:"processes"`
}

type ResolvedProcess struct {
	App              string
	Command          string
	Args             []string
	WorkingDirectory string
	Env              map[string]string
	UnsetEnv         []string
	Capabilities     []protocol.Capability
	Sandbox          lifecycle.SandboxPolicy
}

type Plan struct {
	Processes []ResolvedProcess
}

func Write(filename string, topology Topology) error {
	if err := topology.Validate(); err != nil {
		return err
	}
	return atomicfile.WriteJSON(filename, topology, 0o600)
}

func Load(filename string) (Topology, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return Topology{}, fmt.Errorf("read runtime topology: %w", err)
	}
	var topology Topology
	if err := json.Unmarshal(data, &topology); err != nil {
		return Topology{}, fmt.Errorf("decode runtime topology: %w", err)
	}
	if err := topology.Validate(); err != nil {
		return Topology{}, err
	}
	return topology, nil
}

func (topology Topology) Validate() error {
	if topology.Schema != Schema || len(topology.Processes) == 0 {
		return fmt.Errorf("runtime topology requires schema 1 and at least one process")
	}
	seen := make(map[string]struct{}, len(topology.Processes))
	for _, process := range topology.Processes {
		if !appPattern.MatchString(process.App) {
			return fmt.Errorf("invalid runtime app %q", process.App)
		}
		if _, exists := seen[process.App]; exists {
			return fmt.Errorf("duplicate runtime app %q", process.App)
		}
		seen[process.App] = struct{}{}
		if err := validateRelative(process.Command, "command"); err != nil {
			return fmt.Errorf("runtime app %s: %w", process.App, err)
		}
		if err := validateRelative(process.WorkingDirectory, "workingDirectory"); err != nil {
			return fmt.Errorf("runtime app %s: %w", process.App, err)
		}
		for name, value := range process.Env {
			if !envPattern.MatchString(name) || strings.ContainsRune(value, '\x00') {
				return fmt.Errorf("runtime app %s has invalid environment entry %q", process.App, name)
			}
		}
		unset := make(map[string]struct{}, len(process.UnsetEnv))
		for _, name := range process.UnsetEnv {
			if !envPattern.MatchString(name) {
				return fmt.Errorf("runtime app %s has invalid unset environment name %q", process.App, name)
			}
			if _, duplicate := unset[strings.ToUpper(name)]; duplicate {
				return fmt.Errorf("runtime app %s repeats unset environment name %q", process.App, name)
			}
			unset[strings.ToUpper(name)] = struct{}{}
		}
		capabilities := make(map[protocol.Capability]struct{}, len(process.Capabilities))
		for _, capability := range process.Capabilities {
			if capability != protocol.CapabilityUpdateTransition {
				return fmt.Errorf("runtime app %s requests unsupported capability %q", process.App, capability)
			}
			if _, duplicate := capabilities[capability]; duplicate {
				return fmt.Errorf("runtime app %s repeats capability %q", process.App, capability)
			}
			capabilities[capability] = struct{}{}
		}
		if process.Sandbox != lifecycle.SandboxDefault && process.Sandbox != lifecycle.SandboxChromium {
			return fmt.Errorf("runtime app %s requests unsupported sandbox policy %q", process.App, process.Sandbox)
		}
	}
	return nil
}

func Resolve(filename string) (Plan, error) {
	topology, err := Load(filename)
	if err != nil {
		return Plan{}, err
	}
	root, err := filepath.Abs(filepath.Dir(filename))
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Processes: make([]ResolvedProcess, 0, len(topology.Processes))}
	for _, process := range topology.Processes {
		command, err := resolveContained(root, process.Command, false)
		if err != nil {
			return Plan{}, fmt.Errorf("runtime app %s command: %w", process.App, err)
		}
		workingDirectory, err := resolveContained(root, process.WorkingDirectory, true)
		if err != nil {
			return Plan{}, fmt.Errorf("runtime app %s working directory: %w", process.App, err)
		}
		plan.Processes = append(plan.Processes, ResolvedProcess{
			App: process.App, Command: command, Args: append([]string(nil), process.Args...),
			WorkingDirectory: workingDirectory, Env: cloneMap(process.Env),
			UnsetEnv:     append([]string(nil), process.UnsetEnv...),
			Capabilities: append([]protocol.Capability(nil), process.Capabilities...),
			Sandbox:      process.Sandbox,
		})
	}
	return plan, nil
}

func (plan Plan) Validate() error {
	if len(plan.Processes) == 0 {
		return fmt.Errorf("runtime plan requires at least one process")
	}
	seen := make(map[string]struct{}, len(plan.Processes))
	for _, process := range plan.Processes {
		if !appPattern.MatchString(process.App) || process.Command == "" || process.WorkingDirectory == "" {
			return fmt.Errorf("runtime plan contains an invalid process")
		}
		if _, exists := seen[process.App]; exists {
			return fmt.Errorf("runtime plan repeats app %q", process.App)
		}
		seen[process.App] = struct{}{}
		if info, err := os.Stat(process.Command); err != nil {
			return fmt.Errorf("runtime app %s command is unavailable: %w", process.App, err)
		} else if !info.Mode().IsRegular() {
			return fmt.Errorf("runtime app %s command is not a regular file", process.App)
		}
		if info, err := os.Stat(process.WorkingDirectory); err != nil {
			return fmt.Errorf("runtime app %s working directory is unavailable: %w", process.App, err)
		} else if !info.IsDir() {
			return fmt.Errorf("runtime app %s working directory is not a directory", process.App)
		}
	}
	return nil
}

func Apps(plan Plan) []string {
	apps := make([]string, 0, len(plan.Processes))
	for _, process := range plan.Processes {
		apps = append(apps, process.App)
	}
	sort.Strings(apps)
	return apps
}

func validateRelative(value, field string) error {
	if value == "" || strings.ContainsRune(value, '\\') || path.IsAbs(value) || path.Clean(value) != value {
		return fmt.Errorf("%s must be a clean relative slash path", field)
	}
	if value == ".." || strings.HasPrefix(value, "../") {
		return fmt.Errorf("%s escapes the topology root", field)
	}
	return nil
}

func resolveContained(root, value string, directory bool) (string, error) {
	resolved := filepath.Join(root, filepath.FromSlash(value))
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes topology root")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if directory && !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", value)
	}
	if !directory && !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", value)
	}
	return resolved, nil
}

func cloneMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
