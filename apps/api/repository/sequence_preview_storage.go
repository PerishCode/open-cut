package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type storedSequencePreviewArtifact struct {
	id               domain.ArtifactID
	projectID        domain.ProjectID
	sequenceID       domain.SequenceID
	sequenceRevision domain.Revision
	planDigest       domain.Digest
	rendererVersion  string
	rendererTarget   string
	profile          string
	facts            domain.SequencePreviewMediaFacts
	byteReference    string
	byteSize         uint64
	contentDigest    domain.Digest
}

func (repository *SQLiteProjects) ReconcileSequencePreviewStorage(ctx context.Context) error {
	if err := repository.reconcileSequencePreviewEvictionWork(ctx); err != nil {
		return err
	}
	if err := repository.reconcileSequencePreviewPublicationWork(); err != nil {
		return err
	}
	if err := repository.reconcileSequencePreviewAttemptWork(); err != nil {
		return err
	}
	artifactRoot := filepath.Join(repository.dataDir, "artifacts", "sequence-preview")
	if err := os.MkdirAll(artifactRoot, 0o700); err != nil {
		return err
	}
	records, err := repository.loadStoredSequencePreviewArtifacts(ctx)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(artifactRoot)
	if err != nil {
		return err
	}
	removed := false
	for _, entry := range entries {
		artifactID, parseErr := domain.ParseArtifactID(entry.Name())
		if parseErr != nil {
			continue
		}
		if _, retained := records[artifactID.String()]; retained {
			continue
		}
		if err := os.RemoveAll(filepath.Join(artifactRoot, entry.Name())); err != nil {
			return err
		}
		removed = true
	}
	if removed {
		if err := syncDirectory(artifactRoot); err != nil {
			return err
		}
	}
	for _, record := range records {
		if err := repository.verifyStoredSequencePreviewArtifact(record); err != nil {
			return fmt.Errorf("verify ready sequence preview artifact %s: %w", record.id, err)
		}
	}
	return nil
}

func (repository *SQLiteProjects) reconcileSequencePreviewEvictionWork(ctx context.Context) error {
	repository.artifactLifecycleMu.Lock()
	defer repository.artifactLifecycleMu.Unlock()
	root := filepath.Join(repository.dataDir, "work", "sequence-preview-evictions")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	artifactRoot := filepath.Join(repository.dataDir, "artifacts", "sequence-preview")
	if err := os.MkdirAll(artifactRoot, 0o700); err != nil {
		return err
	}
	for _, eventEntry := range entries {
		if _, err := domain.ParseActivityEventID(eventEntry.Name()); err != nil || !eventEntry.IsDir() {
			continue
		}
		eventRoot := filepath.Join(root, eventEntry.Name())
		artifacts, readErr := os.ReadDir(eventRoot)
		if readErr != nil || len(artifacts) != 1 || !artifacts[0].IsDir() {
			continue
		}
		artifactID, parseErr := domain.ParseArtifactID(artifacts[0].Name())
		if parseErr != nil {
			continue
		}
		var state string
		err := repository.db.QueryRowContext(ctx, `
SELECT state FROM sequence_preview_artifacts WHERE id = ?`, artifactID.String()).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		quarantineRoot := filepath.Join(eventRoot, artifactID.String())
		canonicalRoot := filepath.Join(artifactRoot, artifactID.String())
		switch domain.SequencePreviewArtifactState(state) {
		case domain.SequencePreviewArtifactReady:
			if _, err := os.Lstat(canonicalRoot); !os.IsNotExist(err) {
				continue
			}
			if err := os.Rename(quarantineRoot, canonicalRoot); err != nil {
				return err
			}
			if err := os.Remove(eventRoot); err != nil {
				return err
			}
			if err := syncDirectory(artifactRoot); err != nil {
				return err
			}
		case domain.SequencePreviewArtifactEvicted:
			if err := os.RemoveAll(eventRoot); err != nil {
				return err
			}
		default:
			return application.ErrSequencePreviewInvalid
		}
	}
	return syncDirectory(root)
}

func (repository *SQLiteProjects) reconcileSequencePreviewAttemptWork() error {
	root := filepath.Join(repository.dataDir, "work", "sequence-preview-attempts")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	removed := false
	for _, entry := range entries {
		if _, err := domain.ParseJobAttemptID(entry.Name()); err != nil || !entry.IsDir() {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
		removed = true
	}
	if removed {
		return syncDirectory(root)
	}
	return nil
}

func (repository *SQLiteProjects) reconcileSequencePreviewPublicationWork() error {
	root := filepath.Join(repository.dataDir, "work", "sequence-preview-publication")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	removed := false
	for _, entry := range entries {
		name := entry.Name()
		if len(name) != 73 || name[36] != '-' {
			continue
		}
		if _, err := domain.ParseJobAttemptID(name[:36]); err != nil {
			continue
		}
		if _, err := domain.ParseArtifactID(name[37:]); err != nil {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
			return err
		}
		removed = true
	}
	if removed {
		return syncDirectory(root)
	}
	return nil
}

func (repository *SQLiteProjects) loadStoredSequencePreviewArtifacts(
	ctx context.Context,
) (map[string]storedSequencePreviewArtifact, error) {
	rows, err := repository.db.QueryContext(ctx, `
SELECT id, project_id, sequence_id, sequence_revision, render_plan_digest,
       renderer_version, renderer_target, output_profile, facts_json,
       byte_reference, byte_size, content_digest
FROM sequence_preview_artifacts WHERE state = 'ready'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make(map[string]storedSequencePreviewArtifact)
	for rows.Next() {
		var (
			idValue, projectValue, sequenceValue, planValue, factsJSON string
			contentDigestValue                                         string
			revisionValue                                              uint64
			record                                                     storedSequencePreviewArtifact
		)
		if err := rows.Scan(
			&idValue, &projectValue, &sequenceValue, &revisionValue, &planValue,
			&record.rendererVersion, &record.rendererTarget, &record.profile, &factsJSON,
			&record.byteReference, &record.byteSize, &contentDigestValue,
		); err != nil {
			return nil, err
		}
		var parseErr error
		record.id, parseErr = domain.ParseArtifactID(idValue)
		if parseErr == nil {
			record.projectID, parseErr = domain.ParseProjectID(projectValue)
		}
		if parseErr == nil {
			record.sequenceID, parseErr = domain.ParseSequenceID(sequenceValue)
		}
		if parseErr == nil {
			record.sequenceRevision, parseErr = domain.NewRevision(revisionValue)
		}
		if parseErr == nil {
			record.planDigest, parseErr = domain.ParseDigest(planValue)
		}
		if parseErr == nil {
			record.contentDigest, parseErr = domain.ParseDigest(contentDigestValue)
		}
		if parseErr != nil || json.Unmarshal([]byte(factsJSON), &record.facts) != nil ||
			application.ValidateSequencePreviewFacts(record.facts) != nil {
			return nil, application.ErrSequencePreviewInvalid
		}
		records[idValue] = record
	}
	return records, rows.Err()
}

func (repository *SQLiteProjects) verifyStoredSequencePreviewArtifact(
	record storedSequencePreviewArtifact,
) error {
	if record.byteReference != "artifact:sequence-preview/"+record.id.String() {
		return fmt.Errorf("unexpected byte reference")
	}
	root := filepath.Join(repository.dataDir, "artifacts", "sequence-preview", record.id.String())
	if err := requireDirectory(root); err != nil {
		return err
	}
	if err := requireExactDirectoryEntries(root, []string{"manifest.json", "preview.webm"}); err != nil {
		return err
	}
	manifestBytes, err := readBoundedRegularFile(
		filepath.Join(root, "manifest.json"), application.MaximumSequencePreviewManifestSize,
	)
	if err != nil {
		return err
	}
	manifestHash := sha256.Sum256(manifestBytes)
	if record.contentDigest.String() != "sha256:"+hex.EncodeToString(manifestHash[:]) {
		return fmt.Errorf("manifest digest mismatch")
	}
	manifest, err := application.DecodeSequencePreviewArtifactManifest(manifestBytes)
	if err != nil || manifest.ProjectID != record.projectID || manifest.SequenceID != record.sequenceID ||
		manifest.SequenceRevision != record.sequenceRevision || manifest.RenderPlanDigest != record.planDigest ||
		manifest.RendererVersion != record.rendererVersion || manifest.RendererTarget != record.rendererTarget ||
		manifest.Profile != record.profile || manifest.Facts != record.facts {
		return application.ErrSequencePreviewInvalid
	}
	mediaPath := filepath.Join(root, manifest.Media.Path)
	info, err := os.Lstat(mediaPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		uint64(info.Size()) != manifest.Media.ByteSize.Value() ||
		uint64(len(manifestBytes))+manifest.Media.ByteSize.Value() != record.byteSize {
		return fmt.Errorf("preview media shape or size mismatch")
	}
	return nil
}
