package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ResolveElectron(repositoryRoot, payloadWorkspace string) (string, error) {
	packageRoot := filepath.Join(repositoryRoot, "apps", payloadWorkspace, "node_modules", "electron")
	data, err := os.ReadFile(filepath.Join(packageRoot, "path.txt"))
	if err != nil {
		return "", fmt.Errorf("resolve Electron binary: %w", err)
	}
	relative := strings.TrimSpace(string(data))
	if relative == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("electron/path.txt must contain a relative binary path")
	}
	binary := filepath.Join(packageRoot, "dist", relative)
	contained, err := filepath.Rel(packageRoot, binary)
	if err != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("Electron binary escapes its package")
	}
	info, err := os.Stat(binary)
	if err != nil {
		return "", fmt.Errorf("stat Electron binary: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("Electron binary is not a regular file")
	}
	return binary, nil
}
