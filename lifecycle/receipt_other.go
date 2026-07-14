//go:build !darwin

package lifecycle

import "path/filepath"

func DefaultReceiptPath(executable string) string {
	return filepath.Join(filepath.Dir(executable), "install-receipt.json")
}
