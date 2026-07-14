package tool

import "testing"

func TestVersionArguments(t *testing.T) {
	if got := versionArguments("go"); len(got) != 1 || got[0] != "version" {
		t.Fatalf("go version args = %v", got)
	}
	if got := versionArguments("node"); len(got) != 1 || got[0] != "--version" {
		t.Fatalf("node version args = %v", got)
	}
}
