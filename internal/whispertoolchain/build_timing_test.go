package whispertoolchain

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/PerishCode/open-cut/internal/timingreport"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestBuildWritesFailureTimingReport(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "reports", "whisper.json")
	_, err := Build(context.Background(), BuildOptions{
		RepositoryRoot: t.TempDir(),
		Target:         target.Target{},
		TimingReport:   reportPath,
	})
	if err == nil {
		t.Fatal("whisper toolchain build unexpectedly accepted an invalid target")
	}
	report, readErr := timingreport.Read(reportPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if report.Operation != "whisper-toolchain-build" || report.Outcome != timingreport.OutcomeFailed || report.Error == "" {
		t.Fatalf("report=%+v", report)
	}
}
