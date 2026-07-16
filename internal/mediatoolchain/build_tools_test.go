package mediatoolchain

import "testing"

func TestShellBuildPathForOS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		goos  string
		value string
		want  string
	}{
		{name: "non windows", goos: "linux", value: "/work/toolchain/cc", want: "/work/toolchain/cc"},
		{name: "windows drive", goos: "windows", value: `D:\a\open-cut\cc.exe`, want: "/d/a/open-cut/cc.exe"},
		{name: "windows lower drive", goos: "windows", value: `c:\msys64\usr\bin\sh.exe`, want: "/c/msys64/usr/bin/sh.exe"},
		{name: "windows unc", goos: "windows", value: `\\server\share\tool.exe`, want: "//server/share/tool.exe"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := shellBuildPathForOS(test.goos, test.value); got != test.want {
				t.Fatalf("shell build path = %q, want %q", got, test.want)
			}
		})
	}
}
