package install

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/utils/environment"
)

func defaultReceiptPath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return lifecycle.DefaultReceiptPath(executable), nil
}

func RunHost(ctx context.Context, receiptPath string, stdout, stderr io.Writer) error {
	if receiptPath == "" {
		var err error
		receiptPath, err = defaultReceiptPath()
		if err != nil {
			return err
		}
	}
	receipt, err := LoadReceipt(receiptPath)
	if err != nil {
		return fmt.Errorf("load install receipt: %w", err)
	}
	hostPath, err := os.Executable()
	if err != nil {
		return err
	}
	if err := lifecycle.Run(ctx, lifecycle.BootstrapProcess(receipt.LauncherPath, receipt.BootstrapPath, lifecycle.ProcessSpec{
		Stdout: stdout, Stderr: stderr,
		Env: environment.Merge(
			os.Environ(), []string{lifecycle.SignerSocketEnvironment},
			map[string]string{lifecycle.PlatformHostEnvironment: hostPath},
		),
		Profile: lifecycle.ProfileProduction,
	})); err != nil {
		return fmt.Errorf("bootstrap launcher exited: %w", err)
	}
	return nil
}
