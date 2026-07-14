package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/PerishCode/open-cut/utils/target"
)

func TestNativeCapabilitiesRejectCrossHostTargetExplicitly(t *testing.T) {
	host := target.Host()
	other := target.Target{Platform: target.Linux, Arch: host.Arch}
	if other == host {
		other.Platform = target.Win
	}
	if _, err := NativePackagingPolicy(other); !IsUnsupportedCapability(err, CapabilityNativePackaging) {
		t.Fatalf("packaging error=%v", err)
	}
	workspace := t.TempDir()
	if _, err := PrepareNativeInstall(other, workspace, InstallProduct{
		Name: "Fixture", ExecutableName: "Fixture", BundleID: "local.fixture",
	}); !IsUnsupportedCapability(err, CapabilityNativeInstall) {
		t.Fatalf("install error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "install")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsupported install mutated workspace: %v", err)
	}
}

func TestLifecycleRejectsInvalidTargetBeforeAdapting(t *testing.T) {
	invalid := target.Target{Platform: "darwin", Arch: target.ARM64}
	if _, err := NativePackagingPolicy(invalid); err == nil {
		t.Fatal("invalid target reached packaging adapter")
	}
	if _, _, err := LocateElectronPack(t.TempDir(), "Fixture", invalid); err == nil {
		t.Fatal("invalid target reached pack layout adapter")
	}
}
