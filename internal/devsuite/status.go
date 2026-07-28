package devsuite

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/devsession"
	"github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/sidecar/protocol"
)

type MemberReport struct {
	App   string      `json:"app"`
	PID   int         `json:"pid"`
	State MemberState `json:"state"`
	Ready bool        `json:"ready"`
	Log   string      `json:"log"`
}

type StatusReport struct {
	Schema      int            `json:"schema"`
	BaseDir     string         `json:"baseDir"`
	State       string         `json:"state"` // stopped | running-ready | running-degraded | running-starting
	Generation  uint64         `json:"generation,omitempty"`
	Stamp       string         `json:"stamp,omitempty"`
	Members     []MemberReport `json:"members,omitempty"`
	Divergences []string       `json:"divergences,omitempty"`
	// StaleArtifacts warns when a member's recorded command artifacts changed
	// on disk after its generation started; the suite still runs the old
	// build until the next restart. Warning only — never a degradation.
	StaleArtifacts []string         `json:"staleArtifacts,omitempty"`
	Status         *protocol.Status `json:"status,omitempty"`
}

// Status reports the suite's recorded expectations against live process and
// broker truth. It never repairs anything.
func Status(ctx context.Context, baseDir string) (StatusReport, error) {
	paths, err := devsession.ResolveCellPaths(baseDir)
	if err != nil {
		return StatusReport{}, err
	}
	report := StatusReport{Schema: 1, BaseDir: baseDir, State: "stopped"}
	roster, rosterErr := LoadRoster(RosterPath(paths.Runtime))
	if rosterErr != nil && !errors.Is(rosterErr, ErrNoRoster) {
		return StatusReport{}, rosterErr
	}
	if errors.Is(rosterErr, ErrNoRoster) {
		if _, statErr := os.Stat(paths.ControlFile); statErr == nil {
			report.Divergences = append(report.Divergences,
				"a control descriptor exists without a suite roster (legacy resident dev or stale residue)")
		}
		return report, nil
	}
	report.Generation = roster.Stamp.Generation
	report.Stamp = roster.Stamp.String()

	var brokerStatus *protocol.Status
	if observer, loadErr := client.Load(paths.ControlFile, paths.ObserverTokenFile); loadErr == nil {
		if status, statusErr := observer.Status(ctx); statusErr == nil {
			brokerStatus = &status
		}
	}
	ready := map[string]bool{}
	if brokerStatus != nil {
		for _, session := range brokerStatus.Sessions {
			ready[session.App] = session.Ready
		}
	}

	running := 0
	allAppsReady := true
	for _, member := range roster.Members {
		state := VerifyMember(member)
		memberReady := ready[member.App]
		if member.App == ControlApp {
			memberReady = brokerStatus != nil
		}
		report.Members = append(report.Members, MemberReport{
			App: member.App, PID: member.PID, State: state, Ready: memberReady, Log: member.Log,
		})
		switch state {
		case MemberRunning:
			running++
			if member.App != ControlApp && !memberReady {
				allAppsReady = false
			}
		case MemberStopped:
			report.Divergences = append(report.Divergences, fmt.Sprintf("%s member (pid %d) is not running", member.App, member.PID))
			allAppsReady = false
		default:
			report.Divergences = append(report.Divergences,
				fmt.Sprintf("pid %d no longer matches the recorded %s member (%s); it will never be touched", member.PID, member.App, state))
			allAppsReady = false
		}
	}
	if brokerStatus == nil && running > 0 {
		report.Divergences = append(report.Divergences, "broker status is unreachable while members are running")
		allAppsReady = false
	}
	switch {
	case running == 0:
		report.State = "stopped"
		report.Divergences = append(report.Divergences, "roster exists but no member is running; `dev start` will sweep it")
	case allAppsReady:
		report.State = "running-ready"
	case len(report.Divergences) == 0:
		report.State = "running-starting"
	default:
		report.State = "running-degraded"
	}
	report.Status = brokerStatus
	if running > 0 {
		report.StaleArtifacts = staleMemberArtifacts(roster)
	}
	return report, nil
}

// staleMemberArtifacts compares each running member's recorded command
// artifacts against its start instant. It inspects only the argv the roster
// already records, so it stays generic: no manifest interpretation, no app
// semantics — just files that are provably newer than the process using them.
func staleMemberArtifacts(roster Roster) []string {
	var stale []string
	for _, member := range roster.Members {
		for _, candidate := range memberArtifactPaths(member) {
			info, err := os.Stat(candidate)
			if err != nil || info.IsDir() {
				continue
			}
			if info.ModTime().After(member.StartedAt) {
				stale = append(stale, fmt.Sprintf(
					"%s: %s changed at %s, after this generation started; restart to adopt it",
					member.App, candidate, info.ModTime().UTC().Format(time.RFC3339)))
			}
		}
	}
	return stale
}

func memberArtifactPaths(member Member) []string {
	seen := map[string]struct{}{}
	var paths []string
	add := func(value string) {
		if value == "" || strings.HasPrefix(value, "-") {
			return
		}
		resolved := value
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(member.Directory, resolved)
		}
		if _, duplicate := seen[resolved]; duplicate {
			return
		}
		seen[resolved] = struct{}{}
		paths = append(paths, resolved)
	}
	add(member.Executable)
	for _, arg := range member.Args {
		add(arg)
	}
	return paths
}

// Logs snapshots the tail of each selected member's current log file.
func Logs(baseDir, app string, lines int, output io.Writer) error {
	paths, err := devsession.ResolveCellPaths(baseDir)
	if err != nil {
		return err
	}
	roster, err := LoadRoster(RosterPath(paths.Runtime))
	if err != nil {
		return err
	}
	if lines <= 0 {
		lines = 40
	}
	matched := false
	for _, member := range roster.Members {
		if app != "" && member.App != app {
			continue
		}
		matched = true
		fmt.Fprintf(output, "== %s · generation %d · %s\n", member.App, roster.Stamp.Generation, member.Log)
		fmt.Fprintln(output, tailFileLines(member.Log, lines))
	}
	if !matched {
		return fmt.Errorf("no suite member named %q", app)
	}
	return nil
}

func tailFileLines(path string, lines int) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "(unreadable: " + err.Error() + ")"
	}
	return tailString(string(raw), lines)
}
