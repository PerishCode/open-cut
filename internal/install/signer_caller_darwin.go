//go:build darwin

package install

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
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
		processExecutable, parent, err := processExecutableAndParent(ctx, pid)
		if err != nil {
			return fmt.Errorf("attest platform signer caller: %w", err)
		}
		if depth == 0 {
			parentExecutable = processExecutable
		}
		if sameProcessExecutable(processExecutable, executable) {
			foundHost = true
		}
		if sameProcessExecutable(processExecutable, receipt.CLIPath) {
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

func processExecutableAndParent(ctx context.Context, pid int) (string, int, error) {
	executable, err := processExecutable(pid)
	if err != nil {
		return "", 0, fmt.Errorf("read process %d executable: %w", pid, err)
	}
	parentOutput, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "ppid=").Output()
	if err != nil {
		return "", 0, fmt.Errorf("read process %d parent: %w", pid, err)
	}
	parent, err := strconv.Atoi(strings.TrimSpace(string(parentOutput)))
	if err != nil {
		return "", 0, fmt.Errorf("decode process %d parent: %w", pid, err)
	}
	return executable, parent, nil
}

func processExecutable(pid int) (string, error) {
	arguments, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", err
	}
	const argumentCountBytes = 4
	if len(arguments) <= argumentCountBytes {
		return "", fmt.Errorf("process %d has invalid arguments", pid)
	}
	pathBytes := arguments[argumentCountBytes:]
	end := bytes.IndexByte(pathBytes, 0)
	if end <= 0 {
		return "", fmt.Errorf("process %d has no executable path", pid)
	}
	return string(pathBytes[:end]), nil
}

func sameProcessExecutable(actual, expected string) bool {
	if actual == "" || expected == "" {
		return false
	}
	if filepath.Clean(actual) == filepath.Clean(expected) {
		return true
	}
	actualInfo, actualErr := os.Stat(actual)
	expectedInfo, expectedErr := os.Stat(expected)
	return actualErr == nil && expectedErr == nil && os.SameFile(actualInfo, expectedInfo)
}
