//go:build !windows

package lifecycle

import (
	"os"
	"testing"
)

func assertDevelopmentSignerEndpoint(t *testing.T, requested, endpoint string) {
	t.Helper()
	if endpoint != requested {
		t.Fatalf("endpoint=%q requested=%q", endpoint, requested)
	}
	info, err := os.Stat(endpoint)
	if err != nil || info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("socket info=%+v err=%v", info, err)
	}
}
