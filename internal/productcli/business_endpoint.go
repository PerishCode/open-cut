package productcli

import (
	"context"
	"fmt"

	"github.com/PerishCode/open-cut/internal/cell"
	"github.com/PerishCode/open-cut/internal/config"
	"github.com/PerishCode/open-cut/internal/layout"
	"github.com/PerishCode/open-cut/sidecar/client"
)

func resolveAPIEndpoint(ctx context.Context, bootstrapPath string) (string, error) {
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
	observer, err := client.Load(paths.ControlFile, paths.ObserverTokenFile)
	if err != nil {
		return "", err
	}
	status, err := observer.Status(ctx)
	if err != nil {
		return "", err
	}
	for _, session := range status.Sessions {
		if session.App != "api" || !session.Ready {
			continue
		}
		for _, endpoint := range session.Endpoints {
			if endpoint.Name == "http" {
				if err := validateLoopbackEndpoint(endpoint.URL); err != nil {
					return "", err
				}
				return endpoint.URL, nil
			}
		}
	}
	return "", fmt.Errorf("product API is not ready")
}
