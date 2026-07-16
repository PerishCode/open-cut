package domain

import (
	"fmt"
	"strings"

	"golang.org/x/text/language"
)

const MaximumCaptionLanguageBytes = 64

// CaptionLanguage is the explicit, canonical BCP-47 language attached to one
// creative Caption. It is never inferred from the host locale. "und" remains
// an explicit language choice rather than an absent/default value.
type CaptionLanguage string

func ParseCaptionLanguage(value string) (CaptionLanguage, error) {
	if value == "" || len(value) > MaximumCaptionLanguageBytes || strings.TrimSpace(value) != value {
		return "", fmt.Errorf("caption language is invalid")
	}
	tag, err := language.Parse(value)
	if err != nil || tag.String() != value || len(tag.Extensions()) != 0 {
		return "", fmt.Errorf("caption language is not a canonical BCP-47 tag")
	}
	return CaptionLanguage(value), nil
}

func (value CaptionLanguage) String() string { return string(value) }

func (value CaptionLanguage) Validate() error {
	_, err := ParseCaptionLanguage(value.String())
	return err
}
