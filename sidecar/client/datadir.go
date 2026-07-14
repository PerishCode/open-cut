package client

import (
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/PerishCode/open-cut/sidecar/protocol"
)

var appSegment = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

// ResolveDataDir narrows the cell data directory in a launch envelope to the
// directory owned by that sidecar. The launcher remains the sole source of the
// base path; sidecars only append their validated app identity.
func ResolveDataDir(launch protocol.SidecarLaunch) (string, error) {
	if err := launch.Validate(); err != nil {
		return "", err
	}
	if !appSegment.MatchString(launch.App) {
		return "", fmt.Errorf("sidecar app must be a safe path segment")
	}
	resolved := filepath.Join(launch.DataDir, launch.App)
	relative, err := filepath.Rel(launch.DataDir, resolved)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) {
		return "", fmt.Errorf("sidecar data directory escapes its base path")
	}
	return resolved, nil
}
