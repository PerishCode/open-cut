package service

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/PerishCode/open-cut/internal/install"
	"github.com/PerishCode/open-cut/internal/productcli"
	"github.com/PerishCode/open-cut/lifecycle"
)

const developmentAgentCLIVersion = "development"

type AgentCLIResolverConfig struct {
	Profile           lifecycle.Profile
	DataDir           string
	SidecarExecutable string
	Endpoint          string
	Channel           string
	Namespace         string
	Environment       []string
}

// PrepareAgentCLIResolver resolves the one stable product entry that an Agent
// adapter may expose. Installed modes bind to the platform host's receipt.
// Development modes materialize an adapter-private resolver and context.
// Neither branch consults the API process PATH.
func PrepareAgentCLIResolver(config AgentCLIResolverConfig) (string, error) {
	if !cleanAbsoluteAgentPath(config.DataDir) || config.Environment == nil ||
		config.Channel == "" || config.Namespace == "" {
		return "", ErrAgentAdapterIncompatible
	}
	switch config.Profile {
	case lifecycle.ProfileProduction, lifecycle.ProfilePackaged:
		return installedAgentCLI(config)
	case lifecycle.ProfileDevelopment, lifecycle.ProfileHarness:
		return temporaryAgentCLI(config)
	default:
		return "", ErrAgentAdapterIncompatible
	}
}

func installedAgentCLI(config AgentCLIResolverConfig) (string, error) {
	host := exactEnvironmentValue(config.Environment, lifecycle.PlatformHostEnvironment)
	if !cleanAbsoluteAgentPath(host) {
		return "", ErrAgentAdapterIncompatible
	}
	receipt, err := install.LoadReceipt(lifecycle.DefaultReceiptPath(host))
	if err != nil || receipt.Channel != config.Channel || receipt.Namespace != config.Namespace ||
		!sameExecutable(host, receipt.HostPath) || !resolverPathWithin(receipt.InstallRoot, receipt.CLIPath) ||
		!stableCLIName(receipt.CLIPath) {
		return "", ErrAgentAdapterIncompatible
	}
	if _, err := resolveAgentCandidate(receipt.CLIPath); err != nil {
		return "", ErrAgentAdapterIncompatible
	}
	return receipt.CLIPath, nil
}

func temporaryAgentCLI(config AgentCLIResolverConfig) (string, error) {
	if !cleanAbsoluteAgentPath(config.SidecarExecutable) ||
		config.Endpoint == "" {
		return "", ErrAgentAdapterIncompatible
	}
	signerSocket := exactEnvironmentValue(config.Environment, lifecycle.SignerSocketEnvironment)
	if signerSocket == "" || strings.ContainsRune(signerSocket, 0) {
		return "", ErrAgentAdapterIncompatible
	}
	resolverDirectory := filepath.Join(config.DataDir, "agent", codexAdapterDirectory, "resolver")
	if err := os.MkdirAll(resolverDirectory, 0o700); err != nil {
		return "", fmt.Errorf("prepare Agent CLI resolver: %w", err)
	}
	if err := os.Chmod(resolverDirectory, 0o700); err != nil {
		return "", fmt.Errorf("protect Agent CLI resolver: %w", err)
	}
	name := "open-cut"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	resolver := filepath.Join(resolverDirectory, name)
	if err := replaceResolverExecutable(config.SidecarExecutable, resolver); err != nil {
		return "", fmt.Errorf("materialize Agent CLI resolver: %w", err)
	}
	if err := productcli.WriteAgentAdapterContext(resolver, productcli.AgentAdapterContext{
		Schema:       productcli.AgentAdapterContextSchema,
		Endpoint:     config.Endpoint,
		SignerSocket: signerSocket,
		CLIVersion:   developmentAgentCLIVersion,
	}); err != nil {
		return "", fmt.Errorf("configure Agent CLI resolver: %w", err)
	}
	return resolver, nil
}

func replaceResolverExecutable(source, destination string) error {
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".open-cut-resolver-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Remove(temporaryPath); err != nil {
		return err
	}
	// Development normally keeps the checkout and its data surface on one
	// volume. A hard link makes resolver publication constant-space while the
	// streaming fallback preserves correctness for split-volume setups.
	if err := os.Link(source, temporaryPath); err == nil {
		return os.Rename(temporaryPath, destination)
	}
	temporary, err = os.OpenFile(temporaryPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	if err := temporary.Chmod(0o700); err != nil {
		temporary.Close()
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		temporary.Close()
		return err
	}
	_, copyErr := io.Copy(temporary, input)
	closeInputErr := input.Close()
	if copyErr != nil || closeInputErr != nil {
		temporary.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeInputErr
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, destination)
}

func exactEnvironmentValue(environment []string, name string) string {
	value := ""
	for _, entry := range environment {
		key, candidate, found := strings.Cut(entry, "=")
		if found && key == name {
			value = candidate
		}
	}
	return value
}

func sameExecutable(left, right string) bool {
	leftResolved, leftErr := filepath.EvalSymlinks(left)
	rightResolved, rightErr := filepath.EvalSymlinks(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftResolved) == filepath.Clean(rightResolved)
}

func stableCLIName(path string) bool {
	name := filepath.Base(path)
	return name == "open-cut" || name == "open-cut.exe"
}

func resolverPathWithin(root, candidate string) bool {
	if !cleanAbsoluteAgentPath(root) || !cleanAbsoluteAgentPath(candidate) {
		return false
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	physicalRoot, rootErr := filepath.EvalSymlinks(root)
	physicalCandidate, candidateErr := filepath.EvalSymlinks(candidate)
	if rootErr != nil || candidateErr != nil {
		return false
	}
	relative, err = filepath.Rel(physicalRoot, physicalCandidate)
	return err == nil && relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
