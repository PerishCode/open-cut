package delivery

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/PerishCode/open-cut/internal/install"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestUninstallIsIdempotentWhenInstallIsAlreadyGone(t *testing.T) {
	workspace := t.TempDir()
	installRoot := filepath.Join(workspace, "install", "launcher")
	roots := []string{
		filepath.Join(workspace, "user", "bootstrap"),
		filepath.Join(workspace, "user", "store", "beta", "delivery"),
		filepath.Join(workspace, "user", "cache", "beta", "delivery"),
		filepath.Join(workspace, "user", "runtime", "beta", "delivery"),
		filepath.Join(workspace, "user", "logs", "beta", "delivery"),
		filepath.Join(workspace, "user", "data", "open-cut", "beta", "delivery"),
	}
	for _, path := range append([]string{installRoot}, roots...) {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	receiptPath := filepath.Join(workspace, "receipts", "install-receipt.json")
	receipt := install.Receipt{
		Schema: install.ReceiptSchema, Target: target.Host(), InstallRoot: installRoot,
		HostPath: filepath.Join(installRoot, "host"), LauncherPath: filepath.Join(installRoot, "launcher"),
		CLIPath:       filepath.Join(installRoot, "open-cut"),
		BootstrapPath: filepath.Join(roots[0], "bootstrap.json"), ManagedRoots: roots,
		Channel: "beta", Namespace: "delivery", IdentityBackend: install.IdentityBackendDevelopmentFile,
	}
	if err := install.SaveReceipt(receiptPath, receipt); err != nil {
		t.Fatal(err)
	}
	siblingDataDir := filepath.Join(workspace, "user", "data", "open-cut", "beta", "sibling")
	if err := os.MkdirAll(siblingDataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		result, err := Uninstall(context.Background(), receiptPath, workspace, true)
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt+1, err)
		}
		if !result.Purged || !result.BrokerClosed {
			t.Fatalf("attempt %d: %#v", attempt+1, result)
		}
		if _, err := os.Stat(siblingDataDir); err != nil {
			t.Fatalf("attempt %d removed sibling data directory: %v", attempt+1, err)
		}
	}
}
