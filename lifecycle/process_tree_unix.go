//go:build !windows

package lifecycle

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

type processTree struct {
	enabled bool
}

func newProcessTree(command *exec.Cmd, enabled bool) (processTree, error) {
	if enabled {
		if command.SysProcAttr == nil {
			command.SysProcAttr = &syscall.SysProcAttr{}
		}
		command.SysProcAttr.Setpgid = true
	}
	return processTree{enabled: enabled}, nil
}

func (tree processTree) attach(*exec.Cmd) error { return nil }

func (tree processTree) signal(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	if !tree.enabled {
		return command.Process.Signal(os.Interrupt)
	}
	err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func (tree processTree) kill(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	if !tree.enabled {
		return command.Process.Kill()
	}
	err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func (processTree) close() {}
