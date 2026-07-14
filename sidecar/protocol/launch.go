package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/PerishCode/open-cut/utils/environment"
)

func (launch SidecarLaunch) Validate() error {
	if launch.Control.Schema != 1 || launch.Control.Protocol != Version || launch.Control.Address == "" || launch.Control.SessionID == "" {
		return fmt.Errorf("invalid %s control descriptor", Version)
	}
	if launch.App == "" || launch.Token == "" || launch.Channel == "" || launch.Namespace == "" ||
		!filepath.IsAbs(launch.DataDir) || filepath.Clean(launch.DataDir) != launch.DataDir ||
		!launch.Mode.Valid() || !launch.Presentation.Valid() || launch.Source == "" {
		return fmt.Errorf("incomplete %s launch envelope", Version)
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
	return map[string]string{
		SidecarEnvApp:          launch.App,
		SidecarEnvControl:      string(descriptor),
		SidecarEnvToken:        launch.Token,
		SidecarEnvChannel:      launch.Channel,
		SidecarEnvNamespace:    launch.Namespace,
		SidecarEnvDataDir:      launch.DataDir,
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
	launch := SidecarLaunch{
		App:          os.Getenv(SidecarEnvApp),
		Control:      descriptor,
		Token:        os.Getenv(SidecarEnvToken),
		Channel:      os.Getenv(SidecarEnvChannel),
		Namespace:    os.Getenv(SidecarEnvNamespace),
		DataDir:      os.Getenv(SidecarEnvDataDir),
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
