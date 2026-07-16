package productcli

import (
	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/internal/state"
)

func activeVersion(bootstrapPath string) (string, error) {
	bootstrap, err := config.LoadBootstrap(bootstrapPath)
	if err != nil {
		return "", err
	}
	identity, err := cell.New(bootstrap.Channel, bootstrap.Namespace)
	if err != nil {
		return "", err
	}
	paths, err := layout.Resolve(bootstrap.Roots, identity)
	if err != nil {
		return "", err
	}
	runtimeState, err := state.Load(paths.StateFile, identity.Channel)
	if err != nil {
		return "", err
	}
	return runtimeState.Active, nil
}
