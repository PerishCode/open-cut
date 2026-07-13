package release

import "testing"

func TestCanonicalVersionAndStableDisplay(t *testing.T) {
	version, err := ParseVersionForChannel("1.2.3-stable.4", "stable")
	if err != nil {
		t.Fatal(err)
	}
	if got := version.Display(); got != "1.2.3" {
		t.Fatalf("Display() = %q", got)
	}
}

func TestVersionRejectsNonCanonicalValues(t *testing.T) {
	for _, value := range []string{"1.2.3", "01.2.3-beta.1", "1.2.3-Beta.1", "1.2.3-beta", "1.2.3-beta.01"} {
		if _, err := ParseVersion(value); err == nil {
			t.Fatalf("ParseVersion(%q) succeeded", value)
		}
	}
}

func TestVersionRequiresCellChannel(t *testing.T) {
	if _, err := ParseVersionForChannel("1.2.3-preview.1", "stable"); err == nil {
		t.Fatal("mismatched channel succeeded")
	}
}

func TestVersionComparisonUsesCoreThenChannelIteration(t *testing.T) {
	ordered := []string{"1.0.0-beta.1", "1.0.0-beta.2", "1.0.1-beta.1", "1.1.0-beta.1", "2.0.0-beta.1"}
	for index := 1; index < len(ordered); index++ {
		left, _ := ParseVersion(ordered[index-1])
		right, _ := ParseVersion(ordered[index])
		if left.Compare(right) >= 0 || right.Compare(left) <= 0 {
			t.Fatalf("comparison did not order %s before %s", left, right)
		}
	}
}
