package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/PerishCode/open-cut/product/domain"
)

const codexAdapterDirectory = "codex-cli-v1"

type CodexRuntimePaths struct {
	Home    string
	Scratch string
}

type CodexRuntimeStore struct {
	dataDir string
}

func NewCodexRuntimeStore(dataDir, stableCLIExecutable string) (*CodexRuntimeStore, error) {
	if !cleanAbsoluteAgentPath(dataDir) || !cleanAbsoluteAgentPath(stableCLIExecutable) {
		return nil, ErrAgentAdapterIncompatible
	}
	name := filepath.Base(stableCLIExecutable)
	if name != "open-cut" && name != "open-cut.exe" {
		return nil, ErrAgentAdapterIncompatible
	}
	return &CodexRuntimeStore{dataDir: dataDir}, nil
}

func (store *CodexRuntimeStore) Prepare(runID domain.RunID, turnID domain.TurnID) (CodexRuntimePaths, error) {
	if store == nil || runID.IsZero() || turnID.IsZero() {
		return CodexRuntimePaths{}, ErrAgentProcessInvalid
	}
	home := filepath.Join(
		store.dataDir, "agent", codexAdapterDirectory, "runs", runID.String(), "home",
	)
	scratch := filepath.Join(
		store.dataDir, "scratch", "runs", runID.String(), "turns", turnID.String(), "agent",
	)
	for _, root := range []string{home, scratch} {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return CodexRuntimePaths{}, fmt.Errorf("prepare private Agent runtime: %w", err)
		}
		if err := os.Chmod(root, 0o700); err != nil {
			return CodexRuntimePaths{}, fmt.Errorf("protect private Agent runtime: %w", err)
		}
	}
	return CodexRuntimePaths{Home: home, Scratch: scratch}, nil
}

func (store *CodexRuntimeStore) CollectTurn(runID domain.RunID, turnID domain.TurnID) error {
	if store == nil || runID.IsZero() || turnID.IsZero() {
		return ErrAgentProcessInvalid
	}
	return removePrivateRuntime(filepath.Join(
		store.dataDir, "scratch", "runs", runID.String(), "turns", turnID.String(), "agent",
	))
}

func (store *CodexRuntimeStore) CollectRun(runID domain.RunID) error {
	if store == nil || runID.IsZero() {
		return ErrAgentProcessInvalid
	}
	return removePrivateRuntime(filepath.Join(
		store.dataDir, "agent", codexAdapterDirectory, "runs", runID.String(),
	))
}

func removePrivateRuntime(path string) error {
	err := os.RemoveAll(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("collect private Agent runtime: %w", err)
	}
	return nil
}
