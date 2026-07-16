package lifecycle

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/PerishCode/open-cut/utils/atomicfile"
	"github.com/PerishCode/open-cut/utils/target"
)

type InstallProduct struct {
	Name           string
	ExecutableName string
	BundleID       string
}

const PlatformHostEnvironment = "OC_PLATFORM_HOST"

type InstallLayout struct {
	InstallRoot         string
	HostPath            string
	LauncherPath        string
	CLIPath             string
	InternalReceiptPath string
}

func PrepareNativeInstall(buildTarget target.Target, workspace string, product InstallProduct) (InstallLayout, error) {
	if err := buildTarget.Validate(); err != nil {
		return InstallLayout{}, err
	}
	if buildTarget != target.Host() {
		return InstallLayout{}, UnsupportedCapabilityError{Capability: CapabilityNativeInstall, Target: buildTarget}
	}
	if buildTarget.Platform != target.Mac {
		return InstallLayout{}, UnsupportedCapabilityError{Capability: CapabilityNativeInstall, Target: buildTarget}
	}
	if product.Name == "" || product.ExecutableName == "" || product.BundleID == "" {
		return InstallLayout{}, fmt.Errorf("native install requires complete product identity")
	}
	installRoot := filepath.Join(workspace, "install", product.Name+" Launcher.app")
	layout := InstallLayout{
		InstallRoot:         installRoot,
		HostPath:            filepath.Join(installRoot, "Contents", "MacOS", product.ExecutableName),
		LauncherPath:        filepath.Join(installRoot, "Contents", "Resources", "launcher"),
		CLIPath:             filepath.Join(installRoot, "Contents", "MacOS", "open-cut"),
		InternalReceiptPath: filepath.Join(installRoot, "Contents", "Resources", "install-receipt.json"),
	}
	if _, err := os.Lstat(layout.InstallRoot); err == nil {
		return InstallLayout{}, fmt.Errorf("install root already exists: %s", layout.InstallRoot)
	} else if !os.IsNotExist(err) {
		return InstallLayout{}, err
	}
	if err := os.MkdirAll(filepath.Dir(layout.HostPath), 0o755); err != nil {
		return InstallLayout{}, err
	}
	if err := os.MkdirAll(filepath.Dir(layout.LauncherPath), 0o755); err != nil {
		return InstallLayout{}, err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>%s</string>
<key>CFBundleIdentifier</key><string>%s</string>
<key>CFBundleName</key><string>%s Launcher</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleVersion</key><string>1</string>
</dict></plist>
`, product.ExecutableName, product.BundleID, product.Name)
	if err := atomicfile.Write(filepath.Join(layout.InstallRoot, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		return InstallLayout{}, err
	}
	return layout, nil
}

func SignNativeInstall(ctx context.Context, buildTarget target.Target, layout InstallLayout, stdout, stderr io.Writer) error {
	if err := buildTarget.Validate(); err != nil {
		return err
	}
	if buildTarget.Platform != target.Mac {
		return UnsupportedCapabilityError{Capability: CapabilityArtifactSigning, Target: buildTarget}
	}
	codesign, err := exec.LookPath("codesign")
	if err != nil {
		return UnsupportedCapabilityError{Capability: CapabilityArtifactSigning, Target: buildTarget}
	}
	return Run(ctx, ProcessSpec{
		Executable: codesign,
		Args:       []string{"--force", "--deep", "--sign", "-", layout.InstallRoot},
		Stdout:     stdout,
		Stderr:     stderr,
		Profile:    ProfileProduction,
	})
}
