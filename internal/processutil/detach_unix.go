//go:build !windows

package processutil

import (
	"os/exec"
	"syscall"
)

func Detach(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
