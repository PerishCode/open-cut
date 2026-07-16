package domain

import (
	"strings"
	"testing"
)

func TestCaptionLanguageRequiresCanonicalBCP47WithoutLocaleExtensions(t *testing.T) {
	for _, value := range []string{"und", "en", "en-US", "zh-Hans", "zh-Hant-HK", "ja", "yue-Hant"} {
		parsed, err := ParseCaptionLanguage(value)
		if err != nil || parsed.String() != value || parsed.Validate() != nil {
			t.Fatalf("language %q parsed as %q: %v", value, parsed, err)
		}
	}
	for _, value := range []string{
		"", " EN", "en-us", "EN", "iw", "en-u-ca-gregory", "x-private",
		strings.Repeat("a", MaximumCaptionLanguageBytes+1),
	} {
		if parsed, err := ParseCaptionLanguage(value); err == nil {
			t.Fatalf("invalid language %q parsed as %q", value, parsed)
		}
	}
}
