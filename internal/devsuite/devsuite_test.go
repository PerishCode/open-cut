package devsuite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sidecarclient "github.com/PerishCode/open-cut/sidecar/client"
)

func TestStampRoundTripAndArgument(t *testing.T) {
	stamp, err := NewStamp("dev", "default", 3)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseStamp(stamp.String())
	if err != nil {
		t.Fatal(err)
	}
	if parsed != stamp {
		t.Fatalf("parsed=%+v stamp=%+v", parsed, stamp)
	}
	argument := stamp.Argument()
	stripped := sidecarclient.StripCellStampArguments([]string{"openapi", argument, "check"})
	if len(stripped) != 2 || stripped[0] != "openapi" || stripped[1] != "check" {
		t.Fatalf("stripped=%v", stripped)
	}
	if _, err := ParseStamp("dev/default/0/abcd"); err == nil {
		t.Fatal("generation zero must be invalid")
	}
	if _, err := ParseStamp("dev/default/1"); err == nil {
		t.Fatal("missing nonce must be invalid")
	}
}

func TestRosterRoundTripAndSchemaGate(t *testing.T) {
	directory := t.TempDir()
	path := RosterPath(directory)
	if _, err := LoadRoster(path); err != ErrNoRoster {
		t.Fatalf("expected ErrNoRoster, got %v", err)
	}
	stamp, _ := NewStamp("dev", "default", 2)
	roster := Roster{
		Stamp: stamp, BaseDir: "/tmp/cell", RepositoryRoot: "/tmp/repo", CDPPort: 40000,
		Members: []Member{{
			App: ControlApp, Executable: "/bin/oc-control", Args: []string{"dev", "control-member"},
			PID: 4242, CreateTimeMs: 17, StartedAt: time.Now().UTC(), Log: "/tmp/control-g2.log",
		}},
	}
	if err := WriteRoster(path, roster); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRoster(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Stamp != stamp || len(loaded.Members) != 1 || loaded.Members[0].PID != 4242 {
		t.Fatalf("loaded=%+v", loaded)
	}
	if err := os.WriteFile(path, []byte(`{"schema":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRoster(path); err == nil {
		t.Fatal("wrong schema must fail")
	}
}

func TestVerifyMemberFailsClosed(t *testing.T) {
	dead := Member{App: "api", PID: 1 << 30, CreateTimeMs: 12}
	if state := VerifyMember(dead); state != MemberStopped {
		t.Fatalf("state=%v", state)
	}
	self := Member{App: "control", PID: os.Getpid(), CreateTimeMs: 1}
	if state := VerifyMember(self); state != MemberForeign {
		t.Fatalf("mismatched creation time must be foreign, got %v", state)
	}
}

func TestPruneMemberLogsKeepsTwoGenerations(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"api-g1.log", "api-g2.log", "api-g3.log", "control-g1.log", "keep.txt"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	PruneMemberLogs(directory, 3)
	for name, wanted := range map[string]bool{
		"api-g1.log": false, "api-g2.log": true, "api-g3.log": true, "control-g1.log": false, "keep.txt": true,
	} {
		_, err := os.Stat(filepath.Join(directory, name))
		if wanted && err != nil {
			t.Fatalf("%s should survive: %v", name, err)
		}
		if !wanted && err == nil {
			t.Fatalf("%s should be pruned", name)
		}
	}
}

func TestStaleMemberArtifactsFlagsFilesNewerThanTheirProcess(t *testing.T) {
	workDir := t.TempDir()
	entry := filepath.Join(workDir, "dist", "sidecar", "index.js")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("bundle"), 0o644); err != nil {
		t.Fatal(err)
	}
	member := Member{
		App: "web", Executable: "node",
		Args:      []string{"dist/sidecar/index.js", "--cell-stamp=dev/default/3/abc"},
		Directory: workDir, StartedAt: time.Now().Add(time.Hour),
	}
	if stale := staleMemberArtifacts(Roster{Members: []Member{member}}); len(stale) != 0 {
		t.Fatalf("artifact older than the process must not be stale, got %v", stale)
	}
	member.StartedAt = time.Now().Add(-time.Hour)
	stale := staleMemberArtifacts(Roster{Members: []Member{member}})
	if len(stale) != 1 || !strings.Contains(stale[0], "web: ") || !strings.Contains(stale[0], entry) {
		t.Fatalf("expected one stale warning naming the web entry, got %v", stale)
	}
}
