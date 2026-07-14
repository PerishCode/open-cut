package cell

import (
	"path/filepath"
	"testing"
)

func TestIdentitySuffix(t *testing.T) {
	identity, err := New("beta", "alice-1")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("beta", "alice-1")
	if got := identity.Suffix(); got != want {
		t.Fatalf("Suffix() = %q, want %q", got, want)
	}
}

func TestIdentityRejectsUnsafeSegments(t *testing.T) {
	for _, test := range []struct {
		channel   string
		namespace string
	}{
		{"Beta", "default"},
		{"beta", "../other"},
		{"beta", "space here"},
		{"", "default"},
	} {
		if _, err := New(test.channel, test.namespace); err == nil {
			t.Fatalf("New(%q, %q) succeeded", test.channel, test.namespace)
		}
	}
}
