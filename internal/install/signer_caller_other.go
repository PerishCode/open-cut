//go:build !darwin

package install

import (
	"context"
	"fmt"
)

func verifyPlatformSignerCaller(context.Context, string, Receipt, string, []string) error {
	return fmt.Errorf("platform signer caller attestation is unavailable on this platform")
}
