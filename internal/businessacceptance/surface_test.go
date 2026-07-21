package businessacceptance

import (
	"strings"
	"testing"
)

// The expression has to survive a document with no tabs, no buttons, and no
// body text, because the surfaces it must describe include the ones that never
// finished rendering.
func TestSurfaceDescriptionExpressionIsWellFormed(t *testing.T) {
	for _, fragment := range []string{"role=\"tab\"", "button:not(", "disabled", "innerText", "slice(0, 600)"} {
		if !strings.Contains(surfaceDescriptionExpression, fragment) {
			t.Fatalf("surface description expression omits %q", fragment)
		}
	}
	if strings.Count(surfaceDescriptionExpression, "(") != strings.Count(surfaceDescriptionExpression, ")") {
		t.Fatal("surface description expression has unbalanced parentheses")
	}
}
