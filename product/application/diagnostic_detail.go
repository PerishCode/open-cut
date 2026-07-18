package application

import (
	"strings"
	"unicode/utf8"
)

// MaximumDiagnosticDetail bounds the human-readable failure detail persisted
// alongside a terminal error code. It is a diagnostic aid, never control flow:
// the code stays the machine-readable truth; the detail explains it to a human
// or an Agent deciding how to recover.
const MaximumDiagnosticDetail = 2048

// BoundedDiagnosticDetail normalizes an executor failure cause into one bounded
// single-paragraph string: invalid UTF-8 is repaired, control characters
// (including newlines and tabs) collapse to single spaces, surrounding space is
// trimmed, and the result is capped on a rune boundary. Empty input yields "".
func BoundedDiagnosticDetail(value string) string {
	if value == "" {
		return ""
	}
	value = strings.ToValidUTF8(value, "")
	var builder strings.Builder
	builder.Grow(len(value))
	previousSpace := false
	for _, current := range value {
		if current == '\n' || current == '\t' || current == '\r' || current < 0x20 || current == 0x7f {
			current = ' '
		}
		if current == ' ' {
			if previousSpace || builder.Len() == 0 {
				continue
			}
			previousSpace = true
		} else {
			previousSpace = false
		}
		builder.WriteRune(current)
	}
	detail := strings.TrimRight(builder.String(), " ")
	if len(detail) <= MaximumDiagnosticDetail {
		return detail
	}
	bounded := detail[:MaximumDiagnosticDetail]
	for len(bounded) > 0 && !utf8.ValidString(bounded) {
		bounded = bounded[:len(bounded)-1]
	}
	return strings.TrimRight(bounded, " ")
}
