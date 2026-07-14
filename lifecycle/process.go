package lifecycle

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/PerishCode/open-cut/utils/environment"
)

type Profile string

const (
	ProfileProduction  Profile = "production"
	ProfilePackaged    Profile = "packaged"
	ProfileDevelopment Profile = "dev"
	ProfileHarness     Profile = "harness"
)

type SandboxPolicy string

const (
	SandboxDefault  SandboxPolicy = ""
	SandboxChromium SandboxPolicy = "chromium"
)

type Presentation string

const (
	PresentationInteractive Presentation = "interactive"
	PresentationHeadless    Presentation = "headless"
	presentationEnvironment              = "OC_LIFECYCLE_PRESENTATION"
)

type ProcessSpec struct {
	Executable   string
	Args         []string
	Directory    string
	Env          []string
	Stdout       io.Writer
	Stderr       io.Writer
	Profile      Profile
	Presentation Presentation
	Sandbox      SandboxPolicy
	Detached     bool
}

type Process struct {
	command *exec.Cmd
}

func BootstrapProcess(executable, bootstrap string, spec ProcessSpec) ProcessSpec {
	spec.Executable = executable
	spec.Args = []string{"--bootstrap", bootstrap}
	return spec
}

func VersionedProcess(executable, manifest string, spec ProcessSpec) ProcessSpec {
	spec.Executable = executable
	spec.Args = []string{"--role", "l1", "--manifest", manifest}
	return spec
}

func Start(ctx context.Context, spec ProcessSpec) (*Process, error) {
	resolved, err := resolveProcessSpec(spec)
	if err != nil {
		return nil, err
	}
	var command *exec.Cmd
	if resolved.Detached {
		command = exec.Command(resolved.Executable, resolved.Args...)
		applyDetachment(command)
	} else {
		command = exec.CommandContext(ctx, resolved.Executable, resolved.Args...)
	}
	command.Dir = resolved.Directory
	command.Env = resolved.Env
	command.Stdout = resolved.Stdout
	command.Stderr = resolved.Stderr
	if err := command.Start(); err != nil {
		return nil, err
	}
	return &Process{command: command}, nil
}

func Run(ctx context.Context, spec ProcessSpec) error {
	process, err := Start(ctx, spec)
	if err != nil {
		return err
	}
	return process.Wait()
}

func (process *Process) Wait() error {
	if process == nil || process.command == nil {
		return fmt.Errorf("lifecycle process is not running")
	}
	return process.command.Wait()
}

func (process *Process) Kill() error {
	if process == nil || process.command == nil || process.command.Process == nil {
		return nil
	}
	return process.command.Process.Kill()
}

func (process *Process) PID() int {
	if process == nil || process.command == nil || process.command.Process == nil {
		return 0
	}
	return process.command.Process.Pid
}

func resolveProcessSpec(spec ProcessSpec) (ProcessSpec, error) {
	if spec.Executable == "" {
		return ProcessSpec{}, fmt.Errorf("lifecycle process requires an executable")
	}
	if spec.Sandbox != SandboxDefault && spec.Sandbox != SandboxChromium {
		return ProcessSpec{}, fmt.Errorf("unsupported sandbox policy %q", spec.Sandbox)
	}
	if spec.Env == nil {
		spec.Env = os.Environ()
	}
	inheritedPresentation, err := ResolvePresentation(spec.Env)
	if err != nil {
		return ProcessSpec{}, err
	}
	if spec.Presentation == "" {
		spec.Presentation = inheritedPresentation
	} else if spec.Presentation != PresentationInteractive && spec.Presentation != PresentationHeadless {
		return ProcessSpec{}, fmt.Errorf("unsupported presentation %q", spec.Presentation)
	}
	spec.Env = environment.Merge(spec.Env, nil, map[string]string{presentationEnvironment: string(spec.Presentation)})
	if spec.Stdout == nil {
		spec.Stdout = io.Discard
	}
	if spec.Stderr == nil {
		spec.Stderr = io.Discard
	}
	return applyPlatformProcessPolicy(spec), nil
}

func ResolvePresentation(values []string) (Presentation, error) {
	presentation := PresentationInteractive
	for _, entry := range values {
		name, value, found := strings.Cut(entry, "=")
		if found && strings.EqualFold(name, presentationEnvironment) {
			presentation = Presentation(value)
		}
	}
	if presentation != PresentationInteractive && presentation != PresentationHeadless {
		return "", fmt.Errorf("unsupported presentation %q", presentation)
	}
	return presentation, nil
}
