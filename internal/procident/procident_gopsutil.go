package procident

import (
	"math"

	"github.com/shirou/gopsutil/v4/process"
)

// Alive reports whether the pid currently names a running process. On POSIX
// hosts an EPERM probe proves existence: the signal was routed to a live
// process we may not touch.
func Alive(pid int) bool {
	if pid <= 0 || pid > math.MaxInt32 {
		return false
	}
	exists, err := process.PidExists(int32(pid))
	return err == nil && exists
}

// CreateTimeMs returns the kernel-reported process creation time in Unix
// milliseconds. An exact match against a recorded value is proof the pid was
// not recycled since the record was written.
func CreateTimeMs(pid int) (int64, error) {
	if pid <= 0 || pid > math.MaxInt32 {
		return 0, process.ErrorProcessNotRunning
	}
	target := process.Process{Pid: int32(pid)}
	return target.CreateTime()
}

// ParentPID returns the kernel-reported parent pid.
func ParentPID(pid int) (int, error) {
	if pid <= 0 || pid > math.MaxInt32 {
		return 0, process.ErrorProcessNotRunning
	}
	target := process.Process{Pid: int32(pid)}
	parent, err := target.Ppid()
	if err != nil {
		return 0, err
	}
	return int(parent), nil
}

// Terminate requests a graceful stop; Windows has no cross-process graceful
// signal, so it degrades to a forced stop there. A process that is already
// gone is not an error.
func Terminate(pid int) error {
	return signalProcess(pid, (*process.Process).Terminate)
}

// Kill forcibly stops the process after Terminate ran out of patience.
func Kill(pid int) error {
	return signalProcess(pid, (*process.Process).Kill)
}

func signalProcess(pid int, send func(*process.Process) error) error {
	if pid <= 0 || pid > math.MaxInt32 {
		return nil
	}
	target := process.Process{Pid: int32(pid)}
	err := send(&target)
	if err == nil || !Alive(pid) {
		return nil
	}
	return err
}
