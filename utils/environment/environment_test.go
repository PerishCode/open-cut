package environment

import (
	"strings"
	"testing"
)

func TestMergeRemovesCaseInsensitivelyAndOverrides(t *testing.T) {
	merged := Merge(
		[]string{"PATH=/bin", "electron_run_as_node=1", "MODE=old"},
		[]string{"ELECTRON_RUN_AS_NODE"},
		map[string]string{"MODE": "new", "TOKEN": "token"},
	)
	joined := strings.Join(merged, "\n")
	if strings.Contains(strings.ToUpper(joined), "ELECTRON_RUN_AS_NODE=") {
		t.Fatalf("removed variable remains: %s", joined)
	}
	if !strings.Contains(joined, "MODE=new") || strings.Contains(joined, "MODE=old") {
		t.Fatalf("override failed: %s", joined)
	}
}
