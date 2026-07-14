package lifecycle

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/PerishCode/open-cut/utils/environment"
	"github.com/PerishCode/open-cut/utils/target"
	"github.com/PerishCode/open-cut/utils/tool"
)

type PackagingPolicy struct {
	HoistedDependencyLayout bool
	MaterializeLinks        bool
}

type RuntimeLayout struct {
	ElectronCommand  string
	NodeCommand      string
	PayloadResources string
}

func NativePackagingPolicy(buildTarget target.Target) (PackagingPolicy, error) {
	if err := buildTarget.Validate(); err != nil {
		return PackagingPolicy{}, err
	}
	if buildTarget != target.Host() {
		return PackagingPolicy{}, UnsupportedCapabilityError{Capability: CapabilityNativePackaging, Target: buildTarget}
	}
	policy := PackagingPolicy{}
	if buildTarget.Platform == target.Win {
		policy.HoistedDependencyLayout = true
		policy.MaterializeLinks = true
	}
	return policy, nil
}

func ElectronBuilderConfig(productName, electronVersion, appRoot, outputRoot, resourcesRoot string) map[string]any {
	return map[string]any{
		"productName":     productName,
		"asar":            false,
		"npmRebuild":      false,
		"electronVersion": electronVersion,
		"directories":     map[string]string{"app": appRoot, "output": outputRoot},
		"files":           []string{"dist/**/*", "package.json", "node_modules/**/*"},
		"extraResources":  []map[string]string{{"from": resourcesRoot, "to": "payload"}},
		"mac":             map[string]any{"identity": nil, "target": "dir"},
		"win":             map[string]any{"target": "dir"},
		"linux":           map[string]any{"executableName": slug(productName), "target": "dir"},
	}
}

func BuildElectronPack(
	ctx context.Context,
	buildTarget target.Target,
	repositoryRoot, packageName, configPath string,
	stdout, stderr io.Writer,
) error {
	if err := buildTarget.Validate(); err != nil {
		return err
	}
	pnpm, err := tool.ResolveRepository(repositoryRoot, "pnpm")
	if err != nil {
		return err
	}
	return Run(ctx, ProcessSpec{
		Executable: pnpm.Executable,
		Args: pnpm.Arguments(
			"--filter", packageName, "exec", "electron-builder", "--dir",
			"--"+string(buildTarget.Platform), "--"+string(buildTarget.Arch),
			"--publish", "never", "--config", configPath,
		),
		Directory: repositoryRoot,
		Env:       environment.Merge(os.Environ(), nil, map[string]string{"CSC_IDENTITY_AUTO_DISCOVERY": "false"}),
		Stdout:    stdout,
		Stderr:    stderr,
		Profile:   ProfileProduction,
	})
}

func LocateElectronPack(output, productName string, buildTarget target.Target) (string, string, error) {
	if err := buildTarget.Validate(); err != nil {
		return "", "", err
	}
	entries, err := os.ReadDir(output)
	if err != nil {
		return "", "", err
	}
	directories := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			directories = append(directories, filepath.Join(output, entry.Name()))
		}
	}
	sort.Strings(directories)
	for _, root := range directories {
		executable := electronExecutable(root, productName, buildTarget)
		if executable == "" {
			continue
		}
		relative, err := filepath.Rel(root, executable)
		if err != nil {
			return "", "", err
		}
		return root, relative, nil
	}
	return "", "", fmt.Errorf("could not locate Electron full pack entry under %s", output)
}

func ResolveRuntimeLayout(packRoot, packEntry string, buildTarget target.Target) (RuntimeLayout, error) {
	if err := buildTarget.Validate(); err != nil {
		return RuntimeLayout{}, err
	}
	layout := RuntimeLayout{
		ElectronCommand:  filepath.ToSlash(filepath.Join("app", packEntry)),
		NodeCommand:      filepath.ToSlash(filepath.Join("app", packEntry)),
		PayloadResources: filepath.ToSlash(filepath.Join("app", "resources", "payload")),
	}
	if buildTarget.Platform == target.Mac {
		executable := filepath.Base(packEntry)
		application := strings.Split(filepath.ToSlash(packEntry), "/Contents/")[0]
		if !strings.HasSuffix(application, ".app") || executable == "" {
			return RuntimeLayout{}, fmt.Errorf("invalid macOS Electron entry %q", packEntry)
		}
		helper := executable + " Helper"
		layout.NodeCommand = filepath.ToSlash(filepath.Join(
			"app", application, "Contents", "Frameworks", helper+".app", "Contents", "MacOS", helper,
		))
		layout.PayloadResources = filepath.ToSlash(filepath.Join("app", application, "Contents", "Resources", "payload"))
	}
	nodeOnDisk := filepath.Join(packRoot, filepath.FromSlash(strings.TrimPrefix(layout.NodeCommand, "app/")))
	if info, err := os.Stat(nodeOnDisk); err != nil || !info.Mode().IsRegular() {
		return RuntimeLayout{}, fmt.Errorf("Electron Node command is unavailable at %s", nodeOnDisk)
	}
	return layout, nil
}

func FinalizeElectronPack(ctx context.Context, packRoot string, buildTarget target.Target, stdout, stderr io.Writer) error {
	if err := buildTarget.Validate(); err != nil {
		return err
	}
	if buildTarget.Platform != target.Mac {
		return nil
	}
	if buildTarget != target.Host() {
		return UnsupportedCapabilityError{Capability: CapabilityArtifactSigning, Target: buildTarget}
	}
	applications, err := filepath.Glob(filepath.Join(packRoot, "*.app"))
	if err != nil {
		return err
	}
	if len(applications) != 1 {
		return fmt.Errorf("expected one macOS application under %s, found %d", packRoot, len(applications))
	}
	codesign, err := exec.LookPath("codesign")
	if err != nil {
		return UnsupportedCapabilityError{Capability: CapabilityArtifactSigning, Target: buildTarget}
	}
	if err := Run(ctx, ProcessSpec{
		Executable: codesign,
		Args:       []string{"--force", "--deep", "--sign", "-", applications[0]},
		Directory:  packRoot,
		Stdout:     stdout,
		Stderr:     stderr,
		Profile:    ProfileProduction,
	}); err != nil {
		return fmt.Errorf("ad-hoc sign Electron full pack: %w", err)
	}
	return nil
}

func electronExecutable(root, productName string, buildTarget target.Target) string {
	switch buildTarget.Platform {
	case target.Mac:
		applications, _ := filepath.Glob(filepath.Join(root, "*.app", "Contents", "MacOS", productName))
		return exactExecutable(applications, productName)
	case target.Win:
		applications, _ := filepath.Glob(filepath.Join(root, "*.exe"))
		return exactExecutable(applications, productName+".exe")
	default:
		entries, _ := os.ReadDir(root)
		candidates := make([]string, 0)
		for _, entry := range entries {
			if info, err := entry.Info(); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 && entry.Name() != "chrome-sandbox" && entry.Name() != "chrome_crashpad_handler" {
				candidates = append(candidates, filepath.Join(root, entry.Name()))
			}
		}
		return exactExecutable(candidates, slug(productName))
	}
}

func exactExecutable(candidates []string, expected string) string {
	for _, candidate := range candidates {
		if filepath.Base(candidate) == expected {
			return candidate
		}
	}
	return ""
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var result []rune
	separator := false
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			if separator && len(result) > 0 {
				result = append(result, '-')
			}
			result = append(result, character)
			separator = false
		} else {
			separator = true
		}
	}
	if len(result) == 0 {
		return "payload"
	}
	return string(result)
}
