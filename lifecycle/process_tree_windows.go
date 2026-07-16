//go:build windows

package lifecycle

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type processTree struct {
	enabled bool
	job     windows.Handle
}

func newProcessTree(command *exec.Cmd, enabled bool) (processTree, error) {
	if !enabled {
		return processTree{}, nil
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return processTree{}, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job)
		return processTree{}, err
	}
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP
	return processTree{enabled: true, job: job}, nil
}

func (tree processTree) attach(command *exec.Cmd) error {
	if !tree.enabled {
		return nil
	}
	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_INFORMATION,
		false, uint32(command.Process.Pid),
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	return windows.AssignProcessToJobObject(tree.job, process)
}

func (tree processTree) signal(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	if !tree.enabled {
		return command.Process.Kill()
	}
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(command.Process.Pid))
}

func (tree processTree) kill(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	if !tree.enabled {
		return command.Process.Kill()
	}
	return windows.TerminateJobObject(tree.job, 1)
}

func (tree processTree) close() {
	if tree.enabled && tree.job != 0 {
		_ = windows.CloseHandle(tree.job)
	}
}
