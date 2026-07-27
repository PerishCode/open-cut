// Package devsuite makes the development cell a suite of detached, stamped
// member processes controlled by bounded commands. One control member hosts
// the cell broker and development signer; the app members come from runtime
// topology. dev and the launcher are isomorphic conductors of the same cell
// shape; nothing here stays resident and nothing supervises: crashes surface
// in status and restart heals.
package devsuite

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/procident"
	sidecarclient "github.com/PerishCode/open-cut/sidecar/client"
	"github.com/PerishCode/open-cut/utils/atomicfile"
)

const (
	rosterFileName = "suite.json"
	rosterSchema   = 1
	// ControlApp names the control member (broker + development signer) in
	// the roster; app members keep their topology names.
	ControlApp = "control"

	terminateGrace = 4 * time.Second
	killGrace      = 2 * time.Second
)

// Stamp identifies one generation of one cell's suite. Its argv form is the
// human-visible membership marker; the roster's kernel identity records are
// the fail-closed verifier.
type Stamp struct {
	Channel    string `json:"channel"`
	Namespace  string `json:"namespace"`
	Generation uint64 `json:"generation"`
	Nonce      string `json:"nonce"`
}

func NewStamp(channel, namespace string, generation uint64) (Stamp, error) {
	nonce := make([]byte, 4)
	if _, err := rand.Read(nonce); err != nil {
		return Stamp{}, err
	}
	return Stamp{Channel: channel, Namespace: namespace, Generation: generation, Nonce: hex.EncodeToString(nonce)}, nil
}

func (stamp Stamp) String() string {
	return fmt.Sprintf("%s/%s/%d/%s", stamp.Channel, stamp.Namespace, stamp.Generation, stamp.Nonce)
}

// Argument renders the argv marker appended to every spawned member.
func (stamp Stamp) Argument() string {
	return sidecarclient.CellStampArgumentPrefix + stamp.String()
}

func ParseStamp(value string) (Stamp, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 4 || parts[0] == "" || parts[1] == "" || parts[3] == "" {
		return Stamp{}, fmt.Errorf("invalid cell stamp %q", value)
	}
	generation, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil || generation == 0 {
		return Stamp{}, fmt.Errorf("invalid cell stamp generation %q", value)
	}
	return Stamp{Channel: parts[0], Namespace: parts[1], Generation: generation, Nonce: parts[3]}, nil
}

// Member records one spawned suite process. PID plus kernel creation time is
// the identity proof; a mismatch means the process is not ours and is never
// touched.
type Member struct {
	App          string    `json:"app"`
	Executable   string    `json:"executable"`
	Args         []string  `json:"args"`
	Directory    string    `json:"directory"`
	PID          int       `json:"pid"`
	CreateTimeMs int64     `json:"createTimeMs,omitempty"`
	StartedAt    time.Time `json:"startedAt"`
	Log          string    `json:"log"`
}

type Roster struct {
	Schema         int      `json:"schema"`
	Stamp          Stamp    `json:"stamp"`
	BaseDir        string   `json:"baseDir"`
	RepositoryRoot string   `json:"repositoryRoot"`
	CDPPort        int      `json:"cdpPort,omitempty"`
	Members        []Member `json:"members"`
}

func RosterPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, rosterFileName)
}

// GenerationPath persists the last used suite generation. Stop leaves it in
// place so restarts keep advancing generations across roster removal.
func GenerationPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, "suite-generation")
}

func LastGeneration(runtimeDir string) uint64 {
	raw, err := os.ReadFile(GenerationPath(runtimeDir))
	if err != nil {
		return 0
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func RecordGeneration(runtimeDir string, generation uint64) error {
	return atomicfile.Write(GenerationPath(runtimeDir), []byte(strconv.FormatUint(generation, 10)+"\n"), 0o600)
}

func WriteRoster(path string, roster Roster) error {
	roster.Schema = rosterSchema
	return atomicfile.WriteJSON(path, roster, 0o600)
}

var ErrNoRoster = errors.New("no development suite roster")

func LoadRoster(path string) (Roster, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Roster{}, ErrNoRoster
	}
	if err != nil {
		return Roster{}, err
	}
	var roster Roster
	if err := json.Unmarshal(raw, &roster); err != nil {
		return Roster{}, fmt.Errorf("parse suite roster: %w", err)
	}
	if roster.Schema != rosterSchema {
		return Roster{}, fmt.Errorf("suite roster schema %d is not %d", roster.Schema, rosterSchema)
	}
	return roster, nil
}

func RemoveRoster(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// MemberState is the fail-closed liveness verdict for one roster member.
type MemberState string

const (
	MemberRunning  MemberState = "running"
	MemberStopped  MemberState = "stopped"
	MemberForeign  MemberState = "foreign" // pid alive but identity mismatch: never touch
	MemberUnproven MemberState = "unproven"
)

func VerifyMember(member Member) MemberState {
	if !procident.Alive(member.PID) {
		return MemberStopped
	}
	if member.CreateTimeMs > 0 {
		created, err := procident.CreateTimeMs(member.PID)
		if err != nil {
			return MemberUnproven
		}
		if created == member.CreateTimeMs {
			return MemberRunning
		}
		return MemberForeign
	}
	executable, err := procident.Executable(member.PID)
	if err != nil {
		return MemberUnproven
	}
	if procident.SameExecutable(executable, member.Executable) {
		return MemberRunning
	}
	return MemberForeign
}

// TerminateMember gracefully stops a verified member, escalating to kill
// after the grace window. It returns the final state.
func TerminateMember(member Member) MemberState {
	state := VerifyMember(member)
	if state != MemberRunning {
		return state
	}
	_ = procident.Terminate(member.PID)
	if waitGone(member.PID, terminateGrace) {
		return MemberStopped
	}
	_ = procident.Kill(member.PID)
	if waitGone(member.PID, killGrace) {
		return MemberStopped
	}
	return MemberRunning
}

func waitGone(pid int, patience time.Duration) bool {
	deadline := time.Now().Add(patience)
	for time.Now().Before(deadline) {
		if !procident.Alive(pid) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return !procident.Alive(pid)
}

// MemberLogPath names one member's per-generation log file.
func MemberLogPath(logDir, app string, generation uint64) string {
	return filepath.Join(logDir, fmt.Sprintf("%s-g%d.log", app, generation))
}

// PruneMemberLogs keeps the current and previous generation of each member's
// logs; anything older goes.
func PruneMemberLogs(logDir string, generation uint64) {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".log") {
			continue
		}
		marker := strings.LastIndex(name, "-g")
		if marker < 0 {
			continue
		}
		fileGeneration, err := strconv.ParseUint(strings.TrimSuffix(name[marker+2:], ".log"), 10, 64)
		if err != nil {
			continue
		}
		if generation > 1 && fileGeneration < generation-1 {
			_ = os.Remove(filepath.Join(logDir, name))
		}
	}
}
