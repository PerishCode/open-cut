package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/PerishCode/open-cut/product/domain"
)

type storedProductResource struct {
	id        domain.ResourceID
	state     string
	byteSize  uint64
	sha256    string
	reference string
}

func (repository *SQLiteProjects) ReconcileProductResourceStorage(ctx context.Context) error {
	repository.artifactLifecycleMu.Lock()
	defer repository.artifactLifecycleMu.Unlock()
	if err := os.RemoveAll(filepath.Join(repository.dataDir, "work", "product-resource-publication")); err != nil {
		return err
	}
	root := filepath.Join(repository.dataDir, "resources", "product")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	records, err := repository.loadStoredProductResources(ctx)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		id, parseErr := domain.ParseResourceID(entry.Name())
		if parseErr != nil {
			continue
		}
		if _, exists := records[id.String()]; exists {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	for _, record := range records {
		resourceRoot := filepath.Join(root, record.id.String())
		if record.state == "invalid" {
			if err := os.RemoveAll(resourceRoot); err != nil {
				return err
			}
			continue
		}
		if err := verifyProductResourceStructure(resourceRoot, record); err != nil {
			if removeErr := os.RemoveAll(resourceRoot); removeErr != nil {
				return errors.Join(err, removeErr)
			}
			if _, updateErr := repository.db.ExecContext(ctx, `
UPDATE product_resources SET state = 'invalid' WHERE id = ? AND state = 'ready'`, record.id.String()); updateErr != nil {
				return errors.Join(err, updateErr)
			}
		}
	}
	return syncDirectory(root)
}

func (repository *SQLiteProjects) loadStoredProductResources(
	ctx context.Context,
) (map[string]storedProductResource, error) {
	rows, err := repository.db.QueryContext(ctx, `
SELECT id, state, byte_size, content_digest, byte_reference FROM product_resources`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]storedProductResource)
	for rows.Next() {
		var idValue string
		var record storedProductResource
		if err := rows.Scan(&idValue, &record.state, &record.byteSize, &record.sha256, &record.reference); err != nil {
			return nil, err
		}
		id, err := domain.ParseResourceID(idValue)
		if err != nil || (record.state != "ready" && record.state != "invalid") || record.byteSize == 0 ||
			record.reference != "resource:product/"+idValue {
			return nil, fmt.Errorf("persisted product resource is invalid")
		}
		if _, err := domain.ParseDigest(record.sha256); err != nil {
			return nil, err
		}
		record.id = id
		result[idValue] = record
	}
	return result, rows.Err()
}

// verifyProductResourceStructure deliberately keeps cold-start work bounded. The
// publisher verifies the full digest before committing a ready resource; the
// consuming executor must verify it again before use. Startup only rejects a
// missing, aliased, or structurally inconsistent resource.
func verifyProductResourceStructure(root string, record storedProductResource) error {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("product resource root is unavailable")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "content.bin" || entries[0].IsDir() {
		return fmt.Errorf("product resource tree is invalid")
	}
	path := filepath.Join(root, "content.bin")
	info, err = os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		uint64(info.Size()) != record.byteSize {
		return fmt.Errorf("product resource content is invalid")
	}
	return nil
}
