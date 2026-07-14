package target

import (
	"strings"
	"testing"
)

func TestPublicTargetMappings(t *testing.T) {
	tests := []struct{ platform, arch, name, goos, goarch, executable string }{
		{"mac", "arm64", "mac-arm64", "darwin", "arm64", "launcher"},
		{"win", "x64", "win-x64", "windows", "amd64", "launcher.exe"},
		{"linux", "x64", "linux-x64", "linux", "amd64", "launcher"},
	}
	for _, test := range tests {
		value, err := New(test.platform, test.arch)
		if err != nil {
			t.Fatal(err)
		}
		if value.String() != test.name || value.GoOS() != test.goos || value.GoArch() != test.goarch || value.ExecutableName("launcher") != test.executable {
			t.Fatalf("unexpected target mapping for %s-%s: %+v", test.platform, test.arch, value)
		}
	}
}

func TestRejectsInternalTargetVocabulary(t *testing.T) {
	for _, invalid := range [][2]string{{"darwin", "arm64"}, {"windows", "amd64"}, {"mac", "amd64"}} {
		if _, err := New(invalid[0], invalid[1]); err == nil {
			t.Fatalf("accepted internal target vocabulary %v", invalid)
		}
	}
}

func TestGoBuildEnvironmentReplacesPlatformSettings(t *testing.T) {
	value := Target{Platform: Win, Arch: X64}
	environment := value.GoBuildEnvironment([]string{"PATH=fixture", "GOOS=darwin", "goarch=arm64", "CGO_ENABLED=1"})
	joined := strings.Join(environment, "\n")
	for _, expected := range []string{"PATH=fixture", "CGO_ENABLED=0", "GOOS=windows", "GOARCH=amd64"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("environment=%v missing %s", environment, expected)
		}
	}
	if strings.Contains(joined, "GOOS=darwin") || strings.Contains(joined, "goarch=arm64") || strings.Contains(joined, "CGO_ENABLED=1") {
		t.Fatalf("environment retained overridden values: %v", environment)
	}
}
