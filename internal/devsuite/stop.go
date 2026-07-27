package devsuite

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/devsession"
)

type StopReport struct {
	Schema  int            `json:"schema"`
	BaseDir string         `json:"baseDir"`
	Stamp   string         `json:"stamp"`
	Members []MemberReport `json:"members"`
	Skipped []string       `json:"skipped,omitempty"`
}

// Stop terminates the recorded suite fail-closed: apps first, control member
// last so the broker can close its descriptor gracefully. Mismatched pids are
// reported and never touched. Residual control files are removed only after
// the recorded control member is proven dead.
func Stop(baseDir string) (StopReport, error) {
	paths, err := devsession.ResolveCellPaths(baseDir)
	if err != nil {
		return StopReport{}, err
	}
	roster, err := LoadRoster(RosterPath(paths.Runtime))
	if err != nil {
		return StopReport{}, err
	}
	report := StopReport{Schema: 1, BaseDir: baseDir, Stamp: roster.Stamp.String()}
	var control *Member
	for index := range roster.Members {
		member := roster.Members[index]
		if member.App == ControlApp {
			control = &roster.Members[index]
			continue
		}
		report.Members = append(report.Members, stopOne(member, &report))
	}
	if control != nil {
		report.Members = append(report.Members, stopOne(*control, &report))
		if VerifyMember(*control) == MemberStopped {
			awaitRemoval(paths.ControlFile, 3*time.Second)
			for _, residue := range []string{
				paths.ControlFile, paths.OwnerTokenFile, paths.ObserverTokenFile, ConductorTokenPath(paths.Runtime),
			} {
				_ = os.Remove(residue)
			}
		}
	}
	if err := RemoveRoster(RosterPath(paths.Runtime)); err != nil {
		return report, err
	}
	if len(report.Skipped) > 0 {
		return report, fmt.Errorf("left untouched: %s", strings.Join(report.Skipped, "; "))
	}
	return report, nil
}

func stopOne(member Member, report *StopReport) MemberReport {
	state := TerminateMember(member)
	switch state {
	case MemberForeign, MemberUnproven:
		report.Skipped = append(report.Skipped,
			fmt.Sprintf("pid %d no longer matches the recorded %s member (%s)", member.PID, member.App, state))
	case MemberRunning:
		report.Skipped = append(report.Skipped,
			fmt.Sprintf("%s member (pid %d) survived terminate and kill", member.App, member.PID))
	}
	return MemberReport{App: member.App, PID: member.PID, State: state, Log: member.Log}
}

func awaitRemoval(path string, patience time.Duration) {
	deadline := time.Now().Add(patience)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}
