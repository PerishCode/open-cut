package state

import (
	"path/filepath"
	"testing"
)

func TestActivationTransitionsAndPersistence(t *testing.T) {
	current := Empty()
	prepared, err := Prepare(current, "beta", "1.0.0-beta.1")
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := Confirm(prepared, "beta", "1.0.0-beta.1")
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Active != "1.0.0-beta.1" || confirmed.LastGood != confirmed.Active || confirmed.Candidate != "" {
		t.Fatalf("unexpected confirmed state: %+v", confirmed)
	}

	path := filepath.Join(t.TempDir(), "state", "runtime.json")
	if err := Save(path, "beta", confirmed); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path, "beta")
	if err != nil {
		t.Fatal(err)
	}
	if loaded != confirmed {
		t.Fatalf("loaded state %+v, want %+v", loaded, confirmed)
	}
}

func TestRollbackRetainsLastGood(t *testing.T) {
	base := Runtime{Schema: Schema, Generation: 2, Active: "1.0.0-beta.1", LastGood: "1.0.0-beta.1"}
	prepared, err := Prepare(base, "beta", "1.1.0-beta.1")
	if err != nil {
		t.Fatal(err)
	}
	rolledBack, err := Rollback(prepared, "beta")
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.Active != base.LastGood || rolledBack.Candidate != "" {
		t.Fatalf("unexpected rollback: %+v", rolledBack)
	}
}
