package cleaner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gofrs/flock"
)

func TestCleanKnownGeneratedPathsOnly(t *testing.T) {
	root := t.TempDir()
	write := func(relative string) {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod")
	write("pnpm-workspace.yaml")
	write(".tmp/session/state")
	write("apps/web/dist/index.js")
	write("apps/web/src/index.ts")
	write("packages/client/dist/index.js")
	report, err := Clean(root, ScopeAll, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Items) != 3 {
		t.Fatalf("cleaned %d targets, want 3", len(report.Items))
	}
	for _, removed := range []string{".tmp", "apps/web/dist", "packages/client/dist"} {
		if _, err := os.Stat(filepath.Join(root, removed)); !os.IsNotExist(err) {
			t.Fatalf("%s still exists", removed)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "apps/web/src/index.ts")); err != nil {
		t.Fatalf("business source was removed: %v", err)
	}
}

func TestCleanQuickKeepsMediaToolchainAndLiveCells(t *testing.T) {
	root := t.TempDir()
	write := func(relative string) {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("xx"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod")
	write("pnpm-workspace.yaml")
	write(".tmp/oc-control/media-toolchain/mac-arm64/archive.tar.gz")
	write(".tmp/oc-control/harness-sidecars/store/state.json")
	write(".tmp/oc-control/adhoc-debug/dump.bin")
	write(".tmp/stray-capture.webm")
	write(".tmp/oc-control/dev/runtime/dev/default/broker.lock")
	write(".tmp/oc-control/dev/dev/default/api/database/open-cut.db")
	write("apps/web/dist/index.js")

	lock := flock.New(filepath.Join(root, ".tmp/oc-control/dev/runtime/dev/default/broker.lock"))
	acquired, err := lock.TryLock()
	if err != nil || !acquired {
		t.Fatalf("hold test cell lock: acquired=%v err=%v", acquired, err)
	}
	defer func() { _ = lock.Unlock() }()

	report, err := Clean(root, ScopeQuick, false)
	if err != nil {
		t.Fatal(err)
	}
	statuses := make(map[string]string, len(report.Items))
	for _, item := range report.Items {
		relative, relErr := filepath.Rel(root, item.Path)
		if relErr != nil {
			t.Fatal(relErr)
		}
		statuses[filepath.ToSlash(relative)] = item.Status
		if item.Status != "missing" && item.Bytes <= 0 {
			t.Fatalf("item %s reported no disk usage", item.Path)
		}
	}
	expected := map[string]string{
		".tmp/oc-control/media-toolchain":  "protected",
		".tmp/oc-control/dev":              "in-use",
		".tmp/oc-control/harness-sidecars": "removed",
		".tmp/oc-control/adhoc-debug":      "removed",
		".tmp/stray-capture.webm":          "removed",
	}
	for path, status := range expected {
		if statuses[path] != status {
			t.Fatalf("%s status = %q, want %q (all: %v)", path, statuses[path], status, statuses)
		}
	}
	for _, kept := range []string{
		".tmp/oc-control/media-toolchain/mac-arm64/archive.tar.gz",
		".tmp/oc-control/dev/dev/default/api/database/open-cut.db",
		"apps/web/dist/index.js",
	} {
		if _, err := os.Stat(filepath.Join(root, kept)); err != nil {
			t.Fatalf("quick removed protected path %s: %v", kept, err)
		}
	}
	for _, removed := range []string{".tmp/oc-control/harness-sidecars", ".tmp/stray-capture.webm"} {
		if _, err := os.Stat(filepath.Join(root, removed)); !os.IsNotExist(err) {
			t.Fatalf("%s still exists", removed)
		}
	}

	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	report, err = Clean(root, ScopeQuick, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range report.Items {
		if filepath.Base(item.Path) == "dev" && item.Status != "removed" {
			t.Fatalf("released cell status = %q, want removed", item.Status)
		}
	}
}

func TestCleanTempRefusesLiveCell(t *testing.T) {
	root := t.TempDir()
	for _, marker := range []string{"go.mod", "pnpm-workspace.yaml"} {
		if err := os.WriteFile(filepath.Join(root, marker), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lockPath := filepath.Join(root, ".tmp/oc-control/dev/runtime/dev/default/broker.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	lock := flock.New(lockPath)
	acquired, err := lock.TryLock()
	if err != nil || !acquired {
		t.Fatalf("hold test cell lock: acquired=%v err=%v", acquired, err)
	}
	defer func() { _ = lock.Unlock() }()
	report, err := Clean(root, ScopeTemp, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Items) != 1 || report.Items[0].Status != "in-use" {
		t.Fatalf("temp clean over live cell = %+v, want single in-use item", report.Items)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("live cell was removed: %v", err)
	}
}

func TestCleanDryRunAndWorkspaceGuard(t *testing.T) {
	root := t.TempDir()
	if _, err := Clean(root, ScopeTemp, false); err == nil {
		t.Fatal("non-workspace clean succeeded")
	}
	for _, marker := range []string{"go.mod", "pnpm-workspace.yaml"} {
		if err := os.WriteFile(filepath.Join(root, marker), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, ".tmp", "keep")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Clean(root, ScopeTemp, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("dry-run removed path: %v", err)
	}
}
