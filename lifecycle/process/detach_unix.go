//go:build !windows

package process

import (
	"os/exec"
	"syscall"
)

func applyDetachment(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
