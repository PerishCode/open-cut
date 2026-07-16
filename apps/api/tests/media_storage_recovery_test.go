package tests

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestMediaStorageRecoveryRemovesOnlyRecognizedOrphans(t *testing.T) {
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "api")
	store, err := repository.OpenSQLiteProjects(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 15, 18, 0, 0, 0, time.UTC)
	identity := func(offset time.Duration) string {
		value, err := domain.GenerateUUIDv7(now.Add(offset))
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	attemptID := identity(time.Millisecond)
	artifactID := identity(2 * time.Millisecond)
	runID := identity(3 * time.Millisecond)
	turnID := identity(4 * time.Millisecond)
	batchID := identity(5 * time.Millisecond)
	resourceID := identity(6 * time.Millisecond)

	recognized := []string{
		filepath.Join(dataDir, "work", "media-publication", attemptID+"-"+artifactID),
		filepath.Join(dataDir, "artifacts", "media", artifactID),
		filepath.Join(dataDir, "work", "scratch-publication", resourceID),
		filepath.Join(dataDir, "scratch", "runs", runID, "turns", turnID, batchID),
	}
	unknown := []string{
		filepath.Join(dataDir, "work", "media-publication", "future-format"),
		filepath.Join(dataDir, "artifacts", "media", "operator-note"),
		filepath.Join(dataDir, "work", "scratch-publication", "future-format"),
		filepath.Join(dataDir, "scratch", "runs", runID, "turns", turnID, "future-format"),
	}
	for _, path := range append(recognized, unknown...) {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(recognized[3], resourceID+".png"), []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileProductStorage(ctx, now); err != nil {
		t.Fatal(err)
	}
	for _, path := range recognized {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("recognized orphan remained at %s: %v", path, err)
		}
	}
	for _, path := range unknown {
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Fatalf("unknown entry was changed at %s: %v", path, err)
		}
	}
	if err := store.ReconcileProductStorage(ctx, time.Time{}); !errors.Is(err, application.ErrProductStorageInvalid) {
		t.Fatalf("zero recovery instant error=%v", err)
	}
}
