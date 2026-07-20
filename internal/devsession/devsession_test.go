package devsession

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/internal/peerinventory"
	"github.com/PerishCode/open-cut/internal/procident"
	"github.com/PerishCode/open-cut/sidecar/broker"
)

func TestResolveBaseDirUsesRepositorySubcommandAndCell(t *testing.T) {
	repository := t.TempDir()
	resolved, err := ResolveBaseDir(repository, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(repository, ".tmp", "oc-control", "dev", "dev", "default"); resolved != want {
		t.Fatalf("ResolveBaseDir() = %q, want %q", resolved, want)
	}
	if _, err := ResolveBaseDir(repository, filepath.Join(repository, "custom")); err == nil {
		t.Fatal("base directory without the cell suffix was accepted")
	}
}

func TestRunReservesCellBeforeWorkspaceBuild(t *testing.T) {
	repository := t.TempDir()
	baseDir, err := ResolveBaseDir(repository, "")
	if err != nil {
		t.Fatal(err)
	}
	enteredBuild := make(chan struct{})
	releaseBuild := make(chan struct{})
	stoppedBuild := errors.New("stop after reservation test")
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- run(
			context.Background(), repository, baseDir, io.Discard, io.Discard, make(chan Result, 1),
			func(context.Context, string, io.Writer) error {
				close(enteredBuild)
				<-releaseBuild
				return stoppedBuild
			},
		)
	}()
	<-enteredBuild

	var competingBuildCalled atomic.Bool
	err = run(
		context.Background(), repository, baseDir, io.Discard, io.Discard, make(chan Result, 1),
		func(context.Context, string, io.Writer) error {
			competingBuildCalled.Store(true)
			return nil
		},
	)
	if !errors.Is(err, broker.ErrAlreadyRunning) {
		t.Fatalf("competing Run() error = %v, want ErrAlreadyRunning", err)
	}
	if competingBuildCalled.Load() {
		t.Fatal("competing Run() entered workspace build")
	}
	close(releaseBuild)
	if err := <-firstDone; !errors.Is(err, stoppedBuild) {
		t.Fatalf("first Run() error = %v, want build sentinel", err)
	}

	err = run(
		context.Background(), repository, baseDir, io.Discard, io.Discard, make(chan Result, 1),
		func(context.Context, string, io.Writer) error { return stoppedBuild },
	)
	if !errors.Is(err, stoppedBuild) {
		t.Fatalf("Run() after release error = %v, want build sentinel", err)
	}
}

func TestDevSessionHelperProcess(t *testing.T) {
	if os.Getenv("OC_DEVSESSION_HELPER") != "1" {
		return
	}
	time.Sleep(30 * time.Second)
}

func TestRunReapsRecordedPeersOfADeadSessionBeforeBuild(t *testing.T) {
	repository := t.TempDir()
	baseDir, err := ResolveBaseDir(repository, "")
	if err != nil {
		t.Fatal(err)
	}
	paths, err := ResolveCellPaths(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.Runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestDevSessionHelperProcess$")
	command.Env = append(os.Environ(), "OC_DEVSESSION_HELPER=1")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = command.Wait() }()
	t.Cleanup(func() { _ = command.Process.Kill() })
	stalePeer := peerinventory.Peer{
		App: "runtime", PID: command.Process.Pid, Executable: executable, StartedAt: time.Now().UTC(),
	}
	if err := peerinventory.Write(peerinventory.Path(paths.Runtime), []peerinventory.Peer{stalePeer}); err != nil {
		t.Fatal(err)
	}

	stoppedBuild := errors.New("stop after sweep test")
	var aliveAtBuild atomic.Bool
	err = run(
		context.Background(), repository, baseDir, io.Discard, io.Discard, make(chan Result, 1),
		func(context.Context, string, io.Writer) error {
			aliveAtBuild.Store(procident.Alive(stalePeer.PID))
			return stoppedBuild
		},
	)
	if !errors.Is(err, stoppedBuild) {
		t.Fatalf("Run() error = %v, want build sentinel", err)
	}
	if aliveAtBuild.Load() {
		t.Fatal("recorded stale peer was still alive when the workspace build started")
	}
	if _, err := os.Stat(peerinventory.Path(paths.Runtime)); !os.IsNotExist(err) {
		t.Fatalf("peer inventory survived the sweep: %v", err)
	}
}
