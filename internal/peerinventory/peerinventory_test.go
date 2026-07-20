package peerinventory

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/internal/procident"
)

const helperEnvironment = "OC_PEERINVENTORY_HELPER"

func TestPeerInventoryHelperProcess(t *testing.T) {
	if os.Getenv(helperEnvironment) != "1" {
		return
	}
	time.Sleep(30 * time.Second)
}

func startHelper(t *testing.T) (*exec.Cmd, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestPeerInventoryHelperProcess$")
	command.Env = append(os.Environ(), helperEnvironment+"=1")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = command.Wait() }()
	t.Cleanup(func() { _ = command.Process.Kill() })
	return command, executable
}

func waitHelperGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !procident.Alive(pid) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("helper pid %d survived the sweep", pid)
}

func TestSweepReapsVerifiedStalePeer(t *testing.T) {
	command, executable := startHelper(t)
	path := Path(t.TempDir())
	peer := Peer{App: "payload", PID: command.Process.Pid, Executable: executable, StartedAt: time.Now().UTC()}
	if err := Write(path, []Peer{peer}); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	reaped := Sweep(path, &log)
	if len(reaped) != 1 || reaped[0].PID != peer.PID {
		t.Fatalf("reaped = %+v, log = %q", reaped, log.String())
	}
	waitHelperGone(t, peer.PID)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("inventory survived the sweep: %v", err)
	}
}

func TestSweepLeavesMismatchedExecutableAlone(t *testing.T) {
	command, _ := startHelper(t)
	path := Path(t.TempDir())
	other := filepath.Join(t.TempDir(), "impostor")
	if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	peer := Peer{App: "payload", PID: command.Process.Pid, Executable: other, StartedAt: time.Now().UTC()}
	if err := Write(path, []Peer{peer}); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	if reaped := Sweep(path, &log); len(reaped) != 0 {
		t.Fatalf("reaped mismatched peer: %+v", reaped)
	}
	if !procident.Alive(command.Process.Pid) {
		t.Fatal("sweep terminated a process whose identity did not match")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("inventory survived the sweep: %v", err)
	}
}

func TestSweepSkipsDeadPidAndInvalidDocument(t *testing.T) {
	command, executable := startHelper(t)
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	waitHelperGone(t, command.Process.Pid)

	path := Path(t.TempDir())
	peer := Peer{App: "payload", PID: command.Process.Pid, Executable: executable, StartedAt: time.Now().UTC()}
	if err := Write(path, []Peer{peer}); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	if reaped := Sweep(path, &log); len(reaped) != 0 {
		t.Fatalf("reaped dead peer: %+v", reaped)
	}

	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if reaped := Sweep(path, &log); len(reaped) != 0 {
		t.Fatalf("reaped from invalid inventory: %+v", reaped)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid inventory survived the sweep: %v", err)
	}

	if reaped := Sweep(path, &log); reaped != nil {
		t.Fatalf("sweep of a missing inventory returned %+v", reaped)
	}
}
