package harness

import (
	"fmt"
	"path/filepath"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/sidecar/protocol"
)

func harnessInstallation(workspace string, roles []string) (protocol.InstallationAssertion, error) {
	identity, err := harnessInstallationIdentity(workspace, roles)
	if err != nil {
		return protocol.InstallationAssertion{}, err
	}
	return identity.Assertion(), nil
}

func harnessInstallationIdentity(workspace string, roles []string) (lifecycle.DevelopmentInstallationIdentity, error) {
	identity, err := lifecycle.EnsureDevelopmentInstallationIdentity(filepath.Join(workspace, "identity"), roles)
	if err != nil {
		return lifecycle.DevelopmentInstallationIdentity{}, fmt.Errorf("provision harness installation identity: %w", err)
	}
	return identity, nil
}
