package mediatoolchain

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/PerishCode/open-cut/internal/timingreport"
	"github.com/PerishCode/open-cut/utils/target"
)

func TestBuildWritesFailureTimingReport(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "reports", "media.json")
	_, err := Build(context.Background(), BuildOptions{
		RepositoryRoot: t.TempDir(),
		Target:         target.Host(),
		TimingReport:   reportPath,
	})
	if err == nil {
		t.Fatal("media toolchain build unexpectedly accepted a non-repository root")
	}
	report, readErr := timingreport.Read(reportPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if report.Operation != "media-toolchain-build" || report.Outcome != timingreport.OutcomeFailed || report.Error == "" {
		t.Fatalf("report=%+v", report)
	}
}
