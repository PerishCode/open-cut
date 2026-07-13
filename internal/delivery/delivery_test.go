package delivery

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/PerishCode/open-cut/internal/install"
	"github.com/PerishCode/open-cut/internal/target"
)

func TestUninstallIsIdempotentWhenInstallIsAlreadyGone(t *testing.T) {
	workspace := t.TempDir()
	installRoot := filepath.Join(workspace, "install", "launcher")
	roots := []string{
		filepath.Join(workspace, "user", "bootstrap"), filepath.Join(workspace, "user", "store"),
		filepath.Join(workspace, "user", "cache"), filepath.Join(workspace, "user", "runtime"),
		filepath.Join(workspace, "user", "logs"),
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
		BootstrapPath: filepath.Join(roots[0], "bootstrap.json"), ManagedRoots: roots,
		Channel: "beta", Namespace: "delivery",
	}
	if err := install.SaveReceipt(receiptPath, receipt); err != nil {
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
	}
}
