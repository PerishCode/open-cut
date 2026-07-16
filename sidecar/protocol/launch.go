package protocol

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/PerishCode/open-cut/utils/environment"
)

var installationIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var installationRole = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func (assertion InstallationAssertion) Validate() error {
	if assertion.Schema != 1 || assertion.Generation < 1 || !installationIdentity.MatchString(assertion.InstallationID) || len(assertion.Keys) == 0 {
		return fmt.Errorf("invalid installation assertion")
	}
	roles := make(map[string]struct{}, len(assertion.Keys))
	for _, key := range assertion.Keys {
		if !installationRole.MatchString(key.Role) || key.Algorithm != InstallationKeyAlgorithmEd25519 {
			return fmt.Errorf("invalid installation public key")
		}
		decoded, err := base64.StdEncoding.DecodeString(key.PublicKey)
		if err != nil || len(decoded) != ed25519.PublicKeySize {
			return fmt.Errorf("installation public key %q is not base64 Ed25519", key.Role)
		}
		if _, exists := roles[key.Role]; exists {
			return fmt.Errorf("duplicate installation public key role %q", key.Role)
		}
		roles[key.Role] = struct{}{}
	}
	return nil
}

func (launch SidecarLaunch) Validate() error {
	if launch.Control.Schema != 1 || launch.Control.Protocol != Version || launch.Control.Address == "" || launch.Control.SessionID == "" {
		return fmt.Errorf("invalid %s control descriptor", Version)
	}
	if launch.App == "" || launch.Token == "" || launch.Channel == "" || launch.Namespace == "" ||
		!filepath.IsAbs(launch.DataDir) || filepath.Clean(launch.DataDir) != launch.DataDir ||
		!launch.Mode.Valid() || !launch.Presentation.Valid() || launch.Source == "" {
		return fmt.Errorf("incomplete %s launch envelope", Version)
	}
	if err := launch.Installation.Validate(); err != nil {
		return fmt.Errorf("incomplete %s launch envelope: %w", Version, err)
	}
	return nil
}

func LaunchEnvironmentMap(launch SidecarLaunch) (map[string]string, error) {
	if err := launch.Validate(); err != nil {
		return nil, err
	}
	descriptor, err := json.Marshal(launch.Control)
	if err != nil {
		return nil, err
	}
	installation, err := json.Marshal(launch.Installation)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		SidecarEnvApp:          launch.App,
		SidecarEnvControl:      string(descriptor),
		SidecarEnvToken:        launch.Token,
		SidecarEnvChannel:      launch.Channel,
		SidecarEnvNamespace:    launch.Namespace,
		SidecarEnvDataDir:      launch.DataDir,
		SidecarEnvInstallation: string(installation),
		SidecarEnvMode:         string(launch.Mode),
		SidecarEnvPresentation: string(launch.Presentation),
		SidecarEnvSource:       launch.Source,
	}, nil
}

func AppendLaunchEnvironment(base []string, launch SidecarLaunch) ([]string, error) {
	values, err := LaunchEnvironmentMap(launch)
	if err != nil {
		return nil, err
	}
	return environment.Merge(base, nil, values), nil
}

func LoadLaunchEnvironment() (SidecarLaunch, error) {
	var descriptor ControlDescriptor
	if err := json.Unmarshal([]byte(os.Getenv(SidecarEnvControl)), &descriptor); err != nil {
		return SidecarLaunch{}, fmt.Errorf("decode sidecar control descriptor: %w", err)
	}
	var installation InstallationAssertion
	if err := json.Unmarshal([]byte(os.Getenv(SidecarEnvInstallation)), &installation); err != nil {
		return SidecarLaunch{}, fmt.Errorf("decode sidecar installation assertion: %w", err)
	}
	launch := SidecarLaunch{
		App:          os.Getenv(SidecarEnvApp),
		Control:      descriptor,
		Token:        os.Getenv(SidecarEnvToken),
		Channel:      os.Getenv(SidecarEnvChannel),
		Namespace:    os.Getenv(SidecarEnvNamespace),
		DataDir:      os.Getenv(SidecarEnvDataDir),
		Installation: installation,
		Mode:         LifecycleMode(os.Getenv(SidecarEnvMode)),
		Presentation: Presentation(os.Getenv(SidecarEnvPresentation)),
		Source:       os.Getenv(SidecarEnvSource),
	}
	return launch, launch.Validate()
}

func (mode LifecycleMode) Valid() bool {
	switch mode {
	case LifecycleModeProduction, LifecycleModePackaged, LifecycleModeDev, LifecycleModeHarness:
		return true
	default:
		return false
	}
}

func (presentation Presentation) Valid() bool {
	return presentation == PresentationInteractive || presentation == PresentationHeadless
}
