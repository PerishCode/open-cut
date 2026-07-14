//go:build windows

package filesystem

import (
	"fmt"
	"os/exec"
)

func CreateDirectoryLink(target, link string) error {
	output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("create directory junction: %w: %s", err, output)
	}
	return nil
}
