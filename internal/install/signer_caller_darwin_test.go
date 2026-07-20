//go:build darwin

package install

import (
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/PerishCode/open-cut/internal/procident"
)

func TestActiveAppBundleCallerAllowsPackagedProductNameWithoutModelingElectron(t *testing.T) {
	root := "/tmp/store/beta/delivery/versions/0.1.0-beta.1"
	command := root + "/payload/app/Open Cut.app/Contents/MacOS/Open Cut"
	bundle, ok := activeAppBundleFromCommand(command, root)
	if !ok || bundle != root+"/payload/app/Open Cut.app" {
		t.Fatalf("bundle=%q ok=%t", bundle, ok)
	}
	for _, rejected := range []string{
		"/tmp/other/Open Cut.app/Contents/MacOS/Open Cut",
		root + "/payload/app/not-an-app",
		root + "/payload/app/Open Cut.app/Contents/MacOS/",
	} {
		if bundle, ok := activeAppBundleFromCommand(rejected, root); ok {
			t.Fatalf("accepted %q as %q", rejected, bundle)
		}
	}
}

func TestProcessExecutableIgnoresCallerArgvZero(t *testing.T) {
	const helperEnvironment = "OPEN_CUT_SIGNER_CALLER_HELPER"
	if os.Getenv(helperEnvironment) == "1" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestProcessExecutableIgnoresCallerArgvZero$")
	command.Args[0] = "open-cut"
	command.Env = append(os.Environ(), helperEnvironment+"=1")
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	actual, inspectErr := procident.Executable(command.Process.Pid)
	_ = input.Close()
	waitErr := command.Wait()
	if inspectErr != nil || waitErr != nil {
		t.Fatalf("inspect error = %v, wait error = %v", inspectErr, waitErr)
	}
	if !procident.SameExecutable(actual, executable) {
		t.Fatalf("process executable = %q, want %q", actual, executable)
	}
}
