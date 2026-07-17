package devsession

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync/atomic"
	"testing"

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
