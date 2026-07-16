//go:build windows

package lifecycle

import (
	"os"
	"strings"
	"testing"
)

func assertDevelopmentSignerEndpoint(t *testing.T, requested, endpoint string) {
	t.Helper()
	if !strings.HasPrefix(endpoint, windowsPipePrefix) || endpoint == requested {
		t.Fatalf("named pipe endpoint=%q requested=%q", endpoint, requested)
	}
	if _, err := os.Stat(requested); !os.IsNotExist(err) {
		t.Fatalf("named pipe created a filesystem endpoint: %v", err)
	}
}
