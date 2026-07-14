package devbootstrap

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSatisfiesNodeRequirement(t *testing.T) {
	for _, test := range []struct {
		actual      string
		requirement string
		want        bool
	}{
		{actual: "v24.18.0", requirement: "~24", want: true},
		{actual: "24.0.0", requirement: "~24", want: true},
		{actual: "v25.0.0", requirement: "~24", want: false},
		{actual: "v24.18.0", requirement: "24.18.0", want: true},
		{actual: "v24.18.1", requirement: "24.18.0", want: false},
		{actual: "unknown", requirement: "~24", want: false},
	} {
		if got := satisfiesNodeRequirement(test.actual, test.requirement); got != test.want {
			t.Fatalf("satisfiesNodeRequirement(%q, %q) = %t, want %t", test.actual, test.requirement, got, test.want)
		}
	}
}

func TestConfigureHooksIsIdempotentAndProtectsExistingPolicy(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".githooks"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".githooks", "pre-commit"), []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit := func(arguments ...string) string {
		command := exec.Command("git", arguments...)
		command.Dir = root
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(arguments, " "), err, output)
		}
		return strings.TrimSpace(string(output))
	}
	runGit("init", "--quiet")
	if err := configureHooks(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if err := configureHooks(context.Background(), root); err != nil {
		t.Fatalf("second configure: %v", err)
	}
	if got := runGit("config", "--local", "--get", "core.hooksPath"); got != ".githooks" {
		t.Fatalf("hooks path = %q", got)
	}
	runGit("config", "--local", "core.hooksPath", "custom-hooks")
	if err := configureHooks(context.Background(), root); err == nil || !strings.Contains(err.Error(), "refusing to replace") {
		t.Fatalf("expected protected hook policy error, got %v", err)
	}
}
