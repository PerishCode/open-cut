package tests

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/PerishCode/open-cut/apps/api/model"
	"github.com/PerishCode/open-cut/apps/api/repository"
)

func TestSQLiteProjectsPersistRevisionAndOrderedSnapshot(t *testing.T) {
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "api")
	projects, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dataDir, "database", "open-cut.db"); projects.Path() != want {
		t.Fatalf("database path = %q, want %q", projects.Path(), want)
	}
	for index, project := range []model.Project{
		{ID: "beta", Name: "Beta", Description: "Second"},
		{ID: "alpha", Name: "Alpha", Description: "First"},
		{ID: "alpha", Name: "Alpha updated", Description: "First again"},
	} {
		event, err := projects.Put(ctx, project)
		if err != nil {
			projects.Close()
			t.Fatal(err)
		}
		if event.Revision != uint64(index+1) || event.Project != project {
			projects.Close()
			t.Fatalf("event = %+v", event)
		}
	}
	databasePath := projects.Path()
	if err := projects.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	snapshot, err := reopened.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 3 || len(snapshot.Projects) != 2 || snapshot.Projects[0].ID != "alpha" ||
		snapshot.Projects[0].Name != "Alpha updated" || snapshot.Projects[1].ID != "beta" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if databasePath != reopened.Path() {
		t.Fatalf("database path changed across restart: %q -> %q", databasePath, reopened.Path())
	}
}

func TestSQLiteProjectsRejectRewrittenMigrationHistory(t *testing.T) {
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "api")
	projects, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	databasePath := projects.Path()
	if err := projects.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE schema_migrations SET checksum = 'rewritten' WHERE version = 1`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := repository.OpenSQLiteProjects(ctx, dataDir); err == nil {
		reopened.Close()
		t.Fatal("rewritten migration history was accepted")
	}
}

func TestSQLiteProjectsRejectNewerMigrationHistory(t *testing.T) {
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "api")
	projects, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	databasePath := projects.Path()
	if err := projects.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, checksum) VALUES (2, 'future', 'future')`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := repository.OpenSQLiteProjects(ctx, dataDir); err == nil {
		reopened.Close()
		t.Fatal("newer migration history was accepted by the older binary")
	}
}
