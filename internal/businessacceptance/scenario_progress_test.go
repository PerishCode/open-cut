package businessacceptance

import "testing"

// The journey announces each step through progress so a timeout names the step
// that was running rather than only "did not finish". The dispatch must be
// nil-safe, because most callers (the fast lanes) pass no Progressf.
func TestProgressIsNilSafeAndForwards(t *testing.T) {
	var options CreatorToCLIOptions
	options.progress("no observer attached %d", 1) // must not panic

	var got string
	options.Progressf = func(format string, args ...any) {
		got = format
		if len(args) != 1 || args[0].(int) != 7 {
			t.Fatalf("args = %v", args)
		}
	}
	options.progress("observed %d", 7)
	if got != "observed %d" {
		t.Fatalf("forwarded format = %q", got)
	}
}
