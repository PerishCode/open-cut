//go:build darwin

package install

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/PerishCode/open-cut/internal/procident"
)

const (
	firstPartyUIRole = "first-party-ui"
	productCLIRole   = "product-cli"
)

func verifyPlatformSignerCaller(
	ctx context.Context,
	role string,
	receipt Receipt,
	executable string,
	trustedVersionRoots []string,
) error {
	if role != firstPartyUIRole && role != productCLIRole {
		return fmt.Errorf("platform signer role is unavailable")
	}
	pid := os.Getppid()
	foundHost := false
	foundCLI := false
	parentExecutable := ""
	for depth := 0; pid > 1 && depth < 32; depth++ {
		processExecutable, parent, err := processExecutableAndParent(pid)
		if err != nil {
			return fmt.Errorf("attest platform signer caller: %w", err)
		}
		if depth == 0 {
			parentExecutable = processExecutable
		}
		if procident.SameExecutable(processExecutable, executable) {
			foundHost = true
		}
		if procident.SameExecutable(processExecutable, receipt.CLIPath) {
			foundCLI = true
		}
		pid = parent
		if (role == firstPartyUIRole && foundHost) || (role == productCLIRole && foundCLI) {
			break
		}
	}
	switch role {
	case firstPartyUIRole:
		appBundle := ""
		for _, root := range trustedVersionRoots {
			if candidate, ok := activeAppBundleFromCommand(parentExecutable, root); ok {
				appBundle = candidate
				break
			}
		}
		if !foundHost || appBundle == "" {
			return fmt.Errorf("platform signer rejected non-active application descendant")
		}
		if err := exec.CommandContext(ctx, "codesign", "--verify", "--strict", appBundle).Run(); err != nil {
			return fmt.Errorf("platform signer rejected unsigned active application: %w", err)
		}
	case productCLIRole:
		if !foundCLI {
			return fmt.Errorf("platform signer rejected caller outside the stable CLI chain")
		}
	}
	return nil
}

func activeAppBundleFromCommand(command, activeVersionRoot string) (string, bool) {
	root := strings.TrimSuffix(activeVersionRoot, "/") + "/"
	if !strings.HasPrefix(command, root) {
		return "", false
	}
	marker := ".app/Contents/MacOS/"
	index := strings.Index(command, marker)
	if index < len(root) || index+len(marker) >= len(command) {
		return "", false
	}
	executableTail := command[index+len(marker):]
	if executableTail == "" || strings.HasPrefix(executableTail, " ") {
		return "", false
	}
	return command[:index+len(".app")], true
}

func processExecutableAndParent(pid int) (string, int, error) {
	executable, err := procident.Executable(pid)
	if err != nil {
		return "", 0, fmt.Errorf("read process %d executable: %w", pid, err)
	}
	parent, err := procident.ParentPID(pid)
	if err != nil {
		return "", 0, fmt.Errorf("read process %d parent: %w", pid, err)
	}
	return executable, parent, nil
}
