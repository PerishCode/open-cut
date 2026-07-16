//go:build !darwin

package businessacceptance

import "fmt"

func NewNativeSaveDialog() (NativeSaveDialog, error) {
	return nil, fmt.Errorf("target-local native Save dialog automation is not implemented on this platform")
}
