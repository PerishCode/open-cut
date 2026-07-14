package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/PerishCode/open-cut/apps/api/model"
	"github.com/ncruces/go-sqlite3"
	sqliteDriver "github.com/ncruces/go-sqlite3/driver"
)

const databaseFilename = "open-cut.db"

var migrationName = regexp.MustCompile(`^([0-9]{4})_([a-z0-9_]+)\.sql$`)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	Version  int
	Name     string
	Checksum string
	SQL      string
}

type SQLiteProjects struct {
	db   *sql.DB
	path string
}

func OpenSQLiteProjects(ctx context.Context, dataDir string) (*SQLiteProjects, error) {
	if !filepath.IsAbs(dataDir) || filepath.Clean(dataDir) != dataDir {
		return nil, fmt.Errorf("API data directory must be a clean absolute path")
	}
	databaseDir := filepath.Join(dataDir, "database")
	if err := os.MkdirAll(databaseDir, 0o700); err != nil {
		return nil, fmt.Errorf("create API database directory: %w", err)
	}
	databasePath := filepath.Join(databaseDir, databaseFilename)
	db, err := sqliteDriver.Open(databasePath, func(connection *sqlite3.Conn) error {
		return connection.Exec(`
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
PRAGMA synchronous = FULL;
`)
	})
	if err != nil {
		return nil, fmt.Errorf("open SQLite driver: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	repository := &SQLiteProjects{db: db, path: databasePath}
	closeOnError := func(cause error) (*SQLiteProjects, error) {
		return nil, fmt.Errorf("initialize SQLite project repository: %w", cause)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return closeOnError(err)
	}
	if err := os.Chmod(databasePath, 0o600); err != nil {
		_ = db.Close()
		return closeOnError(err)
	}
	if err := repository.migrate(ctx); err != nil {
		_ = db.Close()
		return closeOnError(err)
	}
	return repository, nil
}

func (repository *SQLiteProjects) Close() error {
	return repository.db.Close()
}

func (repository *SQLiteProjects) Path() string {
	return repository.path
}

func (repository *SQLiteProjects) Snapshot(ctx context.Context) (model.ProjectSnapshot, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return model.ProjectSnapshot{}, err
	}
	defer tx.Rollback()
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM project_state WHERE singleton = 1`).Scan(&revision); err != nil {
		return model.ProjectSnapshot{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, name, description FROM projects ORDER BY id`)
	if err != nil {
		return model.ProjectSnapshot{}, err
	}
	defer rows.Close()
	projects := make([]model.Project, 0)
	for rows.Next() {
		var project model.Project
		if err := rows.Scan(&project.ID, &project.Name, &project.Description); err != nil {
			return model.ProjectSnapshot{}, err
		}
		projects = append(projects, project)
	}
	if err := rows.Err(); err != nil {
		return model.ProjectSnapshot{}, err
	}
	if err := rows.Close(); err != nil {
		return model.ProjectSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.ProjectSnapshot{}, err
	}
	return model.ProjectSnapshot{Revision: uint64(revision), Projects: projects}, nil
}

func (repository *SQLiteProjects) Put(ctx context.Context, project model.Project) (model.ProjectUpserted, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return model.ProjectUpserted{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO projects (id, name, description) VALUES (?, ?, ?)
ON CONFLICT(id) DO UPDATE SET name = excluded.name, description = excluded.description
`, project.ID, project.Name, project.Description); err != nil {
		return model.ProjectUpserted{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE project_state SET revision = revision + 1 WHERE singleton = 1`); err != nil {
		return model.ProjectUpserted{}, err
	}
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM project_state WHERE singleton = 1`).Scan(&revision); err != nil {
		return model.ProjectUpserted{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.ProjectUpserted{}, err
	}
	return model.ProjectUpserted{Revision: uint64(revision), Project: project}, nil
}

func (repository *SQLiteProjects) migrate(ctx context.Context) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	if _, err := repository.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  checksum TEXT NOT NULL,
  applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
) STRICT
`); err != nil {
		return fmt.Errorf("create migration history: %w", err)
	}
	rows, err := repository.db.QueryContext(ctx, `SELECT version, name, checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return fmt.Errorf("read migration history: %w", err)
	}
	type appliedMigration struct {
		version  int
		name     string
		checksum string
	}
	applied := make([]appliedMigration, 0)
	for rows.Next() {
		var current appliedMigration
		if err := rows.Scan(&current.version, &current.name, &current.checksum); err != nil {
			rows.Close()
			return err
		}
		applied = append(applied, current)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for index, current := range applied {
		expectedVersion := index + 1
		if current.version != expectedVersion || current.version > len(migrations) {
			return fmt.Errorf("migration history is not a supported well-ordered prefix at version %d", current.version)
		}
		expected := migrations[index]
		if current.name != expected.Name || current.checksum != expected.Checksum {
			return fmt.Errorf("applied migration %04d_%s was rewritten", current.version, current.name)
		}
	}
	for _, pending := range migrations[len(applied):] {
		tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, pending.SQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %04d_%s: %w", pending.Version, pending.Name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, name, checksum) VALUES (?, ?, ?)`,
			pending.Version, pending.Name, pending.Checksum,
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %04d_%s: %w", pending.Version, pending.Name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %04d_%s: %w", pending.Version, pending.Name, err)
		}
	}
	return nil
}

func loadMigrations() ([]migration, error) {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	migrations := make([]migration, 0, len(entries))
	for index, entry := range entries {
		match := migrationName.FindStringSubmatch(entry.Name())
		if entry.IsDir() || match == nil {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		version, _ := strconv.Atoi(match[1])
		if version != index+1 {
			return nil, fmt.Errorf("migration %q breaks the well-ordered sequence", entry.Name())
		}
		content, err := migrationFiles.ReadFile(filepath.ToSlash(filepath.Join("migrations", entry.Name())))
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(content)
		migrations = append(migrations, migration{
			Version: version, Name: match[2], Checksum: hex.EncodeToString(digest[:]), SQL: string(content),
		})
	}
	if len(migrations) == 0 {
		return nil, fmt.Errorf("SQLite repository requires at least one migration")
	}
	return migrations, nil
}
