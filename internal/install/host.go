package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func DefaultReceiptPath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	directory := filepath.Dir(executable)
	if runtime.GOOS == "darwin" {
		return filepath.Clean(filepath.Join(directory, "..", "Resources", "install-receipt.json")), nil
	}
	return filepath.Join(directory, "install-receipt.json"), nil
}

func RunHost(ctx context.Context, receiptPath string, stdout, stderr io.Writer) error {
	if receiptPath == "" {
		var err error
		receiptPath, err = DefaultReceiptPath()
		if err != nil {
			return err
		}
	}
	receipt, err := LoadReceipt(receiptPath)
	if err != nil {
		return fmt.Errorf("load install receipt: %w", err)
	}
	command := exec.CommandContext(ctx, receipt.LauncherPath, "--bootstrap", receipt.BootstrapPath)
	command.Stdout, command.Stderr = stdout, stderr
	command.Env = os.Environ()
	if err := command.Run(); err != nil {
		return fmt.Errorf("bootstrap launcher exited: %w", err)
	}
	return nil
}
