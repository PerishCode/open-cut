package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/PerishCode/open-cut/product/domain"
)

const codexAdapterDirectory = "codex-cli-v1"

type CodexRuntimePaths struct {
	Home    string
	Scratch string
}

type CodexRuntimeStore struct {
	dataDir             string
	stableCLIExecutable string
}

func NewCodexRuntimeStore(dataDir, stableCLIExecutable string) (*CodexRuntimeStore, error) {
	if !cleanAbsoluteAgentPath(dataDir) || !cleanAbsoluteAgentPath(stableCLIExecutable) {
		return nil, ErrAgentAdapterIncompatible
	}
	name := filepath.Base(stableCLIExecutable)
	if name != "open-cut" && name != "open-cut.exe" {
		return nil, ErrAgentAdapterIncompatible
	}
	return &CodexRuntimeStore{dataDir: dataDir, stableCLIExecutable: stableCLIExecutable}, nil
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
	for _, root := range []string{home, filepath.Join(home, "rules"), scratch} {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return CodexRuntimePaths{}, fmt.Errorf("prepare private Agent runtime: %w", err)
		}
		if err := os.Chmod(root, 0o700); err != nil {
			return CodexRuntimePaths{}, fmt.Errorf("protect private Agent runtime: %w", err)
		}
	}
	if err := replacePrivateFile(filepath.Join(home, "config.toml"), store.config()); err != nil {
		return CodexRuntimePaths{}, err
	}
	if err := replacePrivateFile(filepath.Join(home, "rules", "open-cut.rules"), store.rules()); err != nil {
		return CodexRuntimePaths{}, err
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

func (store *CodexRuntimeStore) config() []byte {
	return []byte(strings.Join([]string{
		"default_permissions = \"open-cut-agent\"",
		"approval_policy = \"never\"",
		"allow_login_shell = false",
		"web_search = \"disabled\"",
		"cli_auth_credentials_store = \"keyring\"",
		"",
		"[permissions.open-cut-agent.filesystem]",
		strconv.Quote(":minimal") + " = \"read\"",
		"",
		"[permissions.open-cut-agent.filesystem.\":workspace_roots\"]",
		strconv.Quote(".") + " = \"write\"",
		"",
		"[permissions.open-cut-agent.network]",
		"enabled = false",
		"",
	}, "\n"))
}

func (store *CodexRuntimeStore) rules() []byte {
	name := filepath.Base(store.stableCLIExecutable)
	return []byte(fmt.Sprintf(`prefix_rule(
    pattern = [%q],
    decision = "allow",
    justification = "Open Cut's stable product CLI is the sole product interface",
    match = [%q],
)
`, name, name+" --help"))
}

func replacePrivateFile(path string, content []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".open-cut-agent-*")
	if err != nil {
		return fmt.Errorf("create private Agent config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect private Agent config: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return fmt.Errorf("write private Agent config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync private Agent config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close private Agent config: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish private Agent config: %w", err)
	}
	return nil
}

func removePrivateRuntime(path string) error {
	err := os.RemoveAll(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("collect private Agent runtime: %w", err)
	}
	return nil
}
