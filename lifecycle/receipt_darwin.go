//go:build darwin

package lifecycle

import "path/filepath"

func DefaultReceiptPath(executable string) string {
	return filepath.Clean(filepath.Join(filepath.Dir(executable), "..", "Resources", "install-receipt.json"))
}
