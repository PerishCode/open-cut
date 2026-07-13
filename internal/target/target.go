package target

import (
	"fmt"
	"runtime"
)

type Platform string
type Arch string

const (
	Mac   Platform = "mac"
	Win   Platform = "win"
	Linux Platform = "linux"

	ARM64 Arch = "arm64"
	X64   Arch = "x64"
)

type Target struct {
	Platform Platform `json:"platform"`
	Arch     Arch     `json:"arch"`
}

func New(platform, arch string) (Target, error) {
	value := Target{Platform: Platform(platform), Arch: Arch(arch)}
	if err := value.Validate(); err != nil {
		return Target{}, err
	}
	return value, nil
}

func Host() Target {
	platform := Platform(runtime.GOOS)
	if runtime.GOOS == "darwin" {
		platform = Mac
	} else if runtime.GOOS == "windows" {
		platform = Win
	}
	arch := Arch(runtime.GOARCH)
	if runtime.GOARCH == "amd64" {
		arch = X64
	}
	return Target{Platform: platform, Arch: arch}
}

func (value Target) Validate() error {
	if value.Platform != Mac && value.Platform != Win && value.Platform != Linux {
		return fmt.Errorf("platform must be mac, win, or linux")
	}
	if value.Arch != ARM64 && value.Arch != X64 {
		return fmt.Errorf("arch must be arm64 or x64")
	}
	return nil
}

func (value Target) String() string {
	return string(value.Platform) + "-" + string(value.Arch)
}

func (value Target) GoOS() string {
	switch value.Platform {
	case Mac:
		return "darwin"
	case Win:
		return "windows"
	default:
		return "linux"
	}
}

func (value Target) GoArch() string {
	if value.Arch == X64 {
		return "amd64"
	}
	return "arm64"
}

func (value Target) ElectronPlatformFlag() string {
	return "--" + string(value.Platform)
}

func (value Target) ElectronArchFlag() string {
	return "--" + string(value.Arch)
}

func (value Target) ExecutableName(base string) string {
	if value.Platform == Win {
		return base + ".exe"
	}
	return base
}
