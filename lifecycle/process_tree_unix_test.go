//go:build !windows

package lifecycle

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestContainedProcessCancellationTerminatesDescendants(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	ctx, cancel := context.WithCancel(context.Background())
	process, err := Start(ctx, ProcessSpec{
		Executable: executable, Args: []string{"-test.run=TestLifecycleProcessTreeHelper"},
		Env:     append(os.Environ(), "OC_PROCESS_TREE_HELPER=1", "OC_PROCESS_TREE_PID_FILE="+pidFile),
		Profile: ProfileHarness, Presentation: PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	childPID := waitForChildPID(t, pidFile)
	cancel()
	if err := process.Wait(); err == nil {
		t.Fatal("cancelled process unexpectedly succeeded")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		err := syscall.Kill(childPID, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("descendant process %d survived contained cancellation", childPID)
}

func TestLifecycleProcessTreeHelper(t *testing.T) {
	if os.Getenv("OC_PROCESS_TREE_HELPER") != "1" {
		return
	}
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		os.Exit(91)
	}
	if err := os.WriteFile(
		os.Getenv("OC_PROCESS_TREE_PID_FILE"), []byte(strconv.Itoa(child.Process.Pid)), 0o600,
	); err != nil {
		_ = child.Process.Kill()
		os.Exit(92)
	}
	_ = child.Wait()
	os.Exit(0)
}

func waitForChildPID(t *testing.T, filename string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filename)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("contained helper did not publish its child PID")
	return 0
}
