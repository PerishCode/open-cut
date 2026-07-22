package controlcli

import (
	"fmt"
	"path/filepath"

	"github.com/PerishCode/open-cut/internal/harness"
	"github.com/PerishCode/open-cut/utils/atomicfile"
)

func writeHarnessReports(workspace string, report harness.Report) error {
	reportRoot := filepath.Join(workspace, "reports")
	if err := atomicfile.WriteJSON(filepath.Join(reportRoot, "report.json"), report, 0o600); err != nil {
		return fmt.Errorf("write harness report: %w", err)
	}
	if err := atomicfile.WriteJSON(filepath.Join(reportRoot, "timing.json"), report.TimingReport(), 0o600); err != nil {
		return fmt.Errorf("write harness timing report: %w", err)
	}
	return nil
}
