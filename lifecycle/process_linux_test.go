//go:build linux

package lifecycle

import "testing"

func TestHarnessChromiumSandboxExceptionIsExplicit(t *testing.T) {
	resolved, err := resolveProcessSpec(ProcessSpec{
		Executable: "/tmp/electron", Profile: ProfileHarness, Sandbox: SandboxChromium,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsArgument(resolved.Args, "--no-sandbox") {
		t.Fatal("harness Chromium process did not receive the explicit Linux sandbox exception")
	}
	if !containsArgument(resolved.Args, "--ozone-platform=headless") {
		t.Fatal("harness Chromium process did not receive the headless Linux display backend")
	}

	production, err := resolveProcessSpec(ProcessSpec{
		Executable: "/tmp/electron", Profile: ProfileProduction, Sandbox: SandboxChromium,
	})
	if err != nil {
		t.Fatal(err)
	}
	if containsArgument(production.Args, "--no-sandbox") {
		t.Fatal("production Chromium process must never disable the sandbox")
	}
	if containsArgument(production.Args, "--ozone-platform=headless") {
		t.Fatal("production Chromium process must keep the default display backend")
	}
}
