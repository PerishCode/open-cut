package lifecycle

import (
	"errors"
	"fmt"

	"github.com/PerishCode/open-cut/utils/target"
)

type Capability string

const (
	CapabilityNativeInstall   Capability = "native-install"
	CapabilityNativePackaging Capability = "native-packaging"
	CapabilityArtifactSigning Capability = "artifact-signing"
)

type UnsupportedCapabilityError struct {
	Capability Capability
	Target     target.Target
}

func (err UnsupportedCapabilityError) Error() string {
	return fmt.Sprintf("lifecycle capability %s is unavailable for %s", err.Capability, err.Target)
}

func IsUnsupportedCapability(err error, capability Capability) bool {
	var unsupported UnsupportedCapabilityError
	return errors.As(err, &unsupported) && unsupported.Capability == capability
}
