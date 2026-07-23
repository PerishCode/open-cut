package tests

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/internal/install"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestInstalledAgentCLIResolverUsesPlatformReceiptNotAmbientPATH(t *testing.T) {
	parallelAPITest(t)
	root := t.TempDir()
	installRoot := filepath.Join(root, "install")
	host := executableFixture(t, filepath.Join(installRoot, "host", "Open Cut"))
	cliName := "open-cut"
	if runtime.GOOS == "windows" {
		cliName += ".exe"
	}
	stableCLI := executableFixture(t, filepath.Join(installRoot, "bin", cliName))
	rogueCLI := executableFixture(t, filepath.Join(root, "rogue", cliName))
	receipt := install.Receipt{
		Schema: install.ReceiptSchema, Target: target.Host(), InstallRoot: installRoot,
		HostPath: host, LauncherPath: filepath.Join(installRoot, "launcher"),
		CLIPath: stableCLI, BootstrapPath: filepath.Join(installRoot, "bootstrap.json"),
		ManagedRoots: []string{installRoot}, Channel: "beta", Namespace: "creator",
		IdentityBackend: install.IdentityBackendPlatformSecure,
	}
	if err := install.SaveReceipt(lifecycle.DefaultReceiptPath(host), receipt); err != nil {
		t.Fatal(err)
	}
	resolved, err := service.PrepareAgentCLIResolver(service.AgentCLIResolverConfig{
		Profile: lifecycle.ProfilePackaged, DataDir: filepath.Join(root, "data"),
		Channel: "beta", Namespace: "creator",
		Environment: []string{
			"PATH=" + filepath.Dir(rogueCLI),
			lifecycle.PlatformHostEnvironment + "=" + host,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved != stableCLI {
		t.Fatalf("resolved=%q want=%q", resolved, stableCLI)
	}
}

func TestInstalledAgentCLIResolverRejectsAnotherInstallationReceipt(t *testing.T) {
	parallelAPITest(t)
	root := t.TempDir()
	installRoot := filepath.Join(root, "install")
	host := executableFixture(t, filepath.Join(installRoot, "host", "Open Cut"))
	otherHost := executableFixture(t, filepath.Join(installRoot, "host", "Other"))
	stableCLI := executableFixture(t, filepath.Join(installRoot, "bin", "open-cut"))
	if runtime.GOOS == "windows" {
		stableCLI = executableFixture(t, filepath.Join(installRoot, "bin", "open-cut.exe"))
	}
	if err := install.SaveReceipt(lifecycle.DefaultReceiptPath(host), install.Receipt{
		Schema: install.ReceiptSchema, Target: target.Host(), InstallRoot: installRoot,
		HostPath: otherHost, LauncherPath: filepath.Join(installRoot, "launcher"),
		CLIPath: stableCLI, BootstrapPath: filepath.Join(installRoot, "bootstrap.json"),
		ManagedRoots: []string{installRoot}, Channel: "beta", Namespace: "creator",
		IdentityBackend: install.IdentityBackendPlatformSecure,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := service.PrepareAgentCLIResolver(service.AgentCLIResolverConfig{
		Profile: lifecycle.ProfilePackaged, DataDir: filepath.Join(root, "data"),
		Channel: "beta", Namespace: "creator",
		Environment: []string{lifecycle.PlatformHostEnvironment + "=" + host},
	})
	if !errors.Is(err, service.ErrAgentAdapterIncompatible) {
		t.Fatalf("error=%v", err)
	}
}

func TestDevelopmentAgentCLIResolverMaterializesPrivateProductEntry(t *testing.T) {
	parallelAPITest(t)
	root := t.TempDir()
	source := executableFixture(t, filepath.Join(root, "build", "api-sidecar.exe"))
	rogueCLI := executableFixture(t, filepath.Join(root, "rogue", "open-cut"))
	signer := filepath.Join(root, "runtime", "signer.sock")
	resolved, err := service.PrepareAgentCLIResolver(service.AgentCLIResolverConfig{
		Profile: lifecycle.ProfileDevelopment, DataDir: filepath.Join(root, "data"),
		SidecarExecutable: source, Endpoint: "http://127.0.0.1:42123",
		Channel: "dev", Namespace: "default",
		Environment: []string{
			"PATH=" + filepath.Dir(rogueCLI),
			lifecycle.SignerSocketEnvironment + "=" + signer,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(resolved) != filepath.Join(root, "data", "agent", "codex-cli-v1", "resolver") ||
		filepath.Base(resolved) != filepath.Base(stableCLINameForHost()) {
		t.Fatalf("resolver=%q", resolved)
	}
	sourceBytes, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	resolverBytes, err := os.ReadFile(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if string(sourceBytes) != string(resolverBytes) {
		t.Fatal("temporary resolver does not contain the current product-side executable")
	}
	contextInfo, err := os.Stat(filepath.Join(filepath.Dir(resolved), ".open-cut-agent-context.json"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && contextInfo.Mode().Perm() != 0o600 {
		t.Fatalf("context mode=%o", contextInfo.Mode().Perm())
	}
}

func stableCLINameForHost() string {
	if runtime.GOOS == "windows" {
		return "open-cut.exe"
	}
	return "open-cut"
}
