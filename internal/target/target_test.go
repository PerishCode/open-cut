package target

import "testing"

func TestPublicTargetMappings(t *testing.T) {
	tests := []struct {
		platform, arch                 string
		name, goos, goarch, executable string
	}{
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
			t.Fatalf("unexpected mapping for %s: %#v", test.name, value)
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
