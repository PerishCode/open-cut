//go:build darwin

package businessacceptance

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacOSSaveDialogScriptEscapesAuthorityPath(t *testing.T) {
	script := macOSSaveDialogScript(`/tmp/open cut/quote"folder`, `story"cut.webm`)
	for _, expected := range []string{`keystroke "/tmp/open cut/quote\"folder"`, `story\"cut.webm`} {
		if !strings.Contains(script, expected) {
			t.Fatalf("native Save script omitted escaped input %q", expected)
		}
	}
	if !strings.Contains(script, "exists sheet 1 of window 1") {
		t.Fatal("native Save script does not wait for the installed app sheet")
	}
	compiler, err := exec.LookPath("osacompile")
	if err != nil {
		t.Fatal("macOS AppleScript compiler is unavailable")
	}
	command := exec.Command(compiler, "-o", filepath.Join(t.TempDir(), "native-save.scpt"), "-")
	command.Stdin = strings.NewReader(script)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("native Save script does not compile: %v: %s", err, output)
	}
}
