//go:build darwin

package businessacceptance

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

type macOSNativeSaveDialog struct {
	executable string
}

func NewNativeSaveDialog() (NativeSaveDialog, error) {
	executable, err := exec.LookPath("osascript")
	if err != nil {
		return nil, fmt.Errorf("macOS native dialog automation is unavailable")
	}
	return macOSNativeSaveDialog{executable: executable}, nil
}

func (driver macOSNativeSaveDialog) Select(ctx context.Context, destinationPath string) error {
	if driver.executable == "" || !filepath.IsAbs(destinationPath) || filepath.Clean(destinationPath) != destinationPath {
		return fmt.Errorf("macOS native Save dialog input is invalid")
	}
	command := exec.CommandContext(ctx, driver.executable, "-")
	command.Stdin = strings.NewReader(macOSSaveDialogScript(filepath.Dir(destinationPath), filepath.Base(destinationPath)))
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return fmt.Errorf("macOS native Save dialog automation failed")
	}
	return nil
}

func macOSSaveDialogScript(directory, displayName string) string {
	return fmt.Sprintf(`tell application "System Events"
  set saveProcess to missing value
  repeat 200 times
    set foregroundProcesses to every application process whose frontmost is true
    if (count of foregroundProcesses) > 0 then
      set candidateProcess to item 1 of foregroundProcesses
      try
        if (count of windows of candidateProcess) > 0 then
          if exists sheet 1 of window 1 of candidateProcess then
            set saveProcess to candidateProcess
            exit repeat
          end if
        end if
      end try
    end if
    delay 0.05
  end repeat
  if saveProcess is missing value then error "native Save sheet unavailable"
  tell saveProcess
    keystroke "g" using {command down, shift down}
    delay 0.2
    keystroke %s
    key code 36
    delay 0.3
    try
      set value of text field 1 of sheet 1 of window 1 to %s
    on error
      keystroke "a" using {command down}
      keystroke %s
    end try
    delay 0.1
    key code 36
  end tell
end tell`, appleScriptString(directory), appleScriptString(displayName), appleScriptString(displayName))
}

func appleScriptString(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\r", `\r`, "\n", `\n`).Replace(value) + `"`
}
