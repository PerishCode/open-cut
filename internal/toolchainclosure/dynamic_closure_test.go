package toolchainclosure

import "testing"

// The forbidden-library policy moved here with the check it belongs to. It is
// the reason a Windows build must link the MinGW runtime statically: a shipped
// executable that resolves libgcc or libstdc++ from the build host is not the
// executable that was qualified.
func TestForbiddenPackagedDynamicLibraries(t *testing.T) {
	for _, library := range []string{
		"libgcc_s_seh-1.dll", "libstdc++-6.dll", "libwinpthread-1.dll",
	} {
		if reason := forbiddenPackagedDynamicLibrary(library); reason != "unshipped MinGW runtime library" {
			t.Fatalf("library=%s reason=%q", library, reason)
		}
	}
	for _, library := range []string{
		"libharfbuzz.so.0", "libfribidi.0.dylib", "libfreetype.6.dylib",
	} {
		if reason := forbiddenPackagedDynamicLibrary(library); reason != "pinned native text library" {
			t.Fatalf("library=%s reason=%q", library, reason)
		}
	}
	for _, library := range []string{"KERNEL32.dll", "/usr/lib/libSystem.B.dylib"} {
		if reason := forbiddenPackagedDynamicLibrary(library); reason != "" {
			t.Fatalf("library=%s reason=%q", library, reason)
		}
	}
}
