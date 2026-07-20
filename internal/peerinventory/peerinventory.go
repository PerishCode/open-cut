// Package peerinventory records the peers a runtime host spawned so a later
// session of the same cell can reap residues of a runner that died without
// tearing them down. The inventory is dev/harness substrate state only:
// production launchers own cell lifetime and never write one. Reaping fails
// closed — a recorded pid is only terminated when its kernel-reported
// creation time exactly matches the record, which a recycled pid cannot
// satisfy; records without a creation time fall back to executable identity.
package peerinventory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/PerishCode/open-cut/internal/procident"
	"github.com/PerishCode/open-cut/utils/atomicfile"
)

const (
	inventoryFileName = "peers.json"
	inventorySchema   = 1
	terminateGrace    = 4 * time.Second
	killGrace         = 2 * time.Second
)

type Peer struct {
	App        string    `json:"app"`
	PID        int       `json:"pid"`
	Executable string    `json:"executable"`
	StartedAt  time.Time `json:"startedAt"`
	// CreateTimeMs is the kernel-reported process creation time; an exact
	// match at sweep time proves the pid was not recycled.
	CreateTimeMs int64 `json:"createTimeMs,omitempty"`
}

type document struct {
	Schema int    `json:"schema"`
	Peers  []Peer `json:"peers"`
}

// Path locates the inventory inside a cell runtime directory, next to the
// broker lock it is subordinate to.
func Path(runtimeDir string) string {
	return filepath.Join(runtimeDir, inventoryFileName)
}

func Write(path string, peers []Peer) error {
	if peers == nil {
		peers = []Peer{}
	}
	return atomicfile.WriteJSON(path, document{Schema: inventorySchema, Peers: peers}, 0o600)
}

func Remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Sweep terminates recorded peers that are provably still the recorded
// processes, then removes the inventory: the next session records its own
// truth. Callers must hold the cell broker lock so no live session is writing
// the file concurrently.
func Sweep(path string, stderr io.Writer) []Peer {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		fmt.Fprintf(stderr, "read peer inventory: %v\n", err)
		return nil
	}
	var recorded document
	if err := json.Unmarshal(raw, &recorded); err != nil || recorded.Schema != inventorySchema {
		fmt.Fprintf(stderr, "peer inventory %s is invalid; leaving residual processes alone\n", path)
		_ = Remove(path)
		return nil
	}
	var reaped []Peer
	for _, peer := range recorded.Peers {
		if !procident.Alive(peer.PID) {
			continue
		}
		if !sameRecordedProcess(peer) {
			fmt.Fprintf(stderr, "pid %d no longer matches recorded %s peer; leaving it alone\n", peer.PID, peer.App)
			continue
		}
		fmt.Fprintf(stderr, "reaping stale %s peer pid %d from a previous session\n", peer.App, peer.PID)
		_ = procident.Terminate(peer.PID)
		if !waitGone(peer.PID, terminateGrace) {
			_ = procident.Kill(peer.PID)
			waitGone(peer.PID, killGrace)
		}
		reaped = append(reaped, peer)
	}
	_ = Remove(path)
	return reaped
}

// sameRecordedProcess proves the live pid is still the recorded process.
// Exact kernel creation-time equality is the decisive check; legacy records
// without one fall back to kernel-reported executable identity.
func sameRecordedProcess(peer Peer) bool {
	if peer.CreateTimeMs > 0 {
		created, err := procident.CreateTimeMs(peer.PID)
		return err == nil && created == peer.CreateTimeMs
	}
	executable, err := procident.Executable(peer.PID)
	return err == nil && procident.SameExecutable(executable, peer.Executable)
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
