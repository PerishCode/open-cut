package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type storedSequenceExportArtifact struct {
	id               domain.ArtifactID
	producerJobID    domain.WorkJobID
	projectID        domain.ProjectID
	sequenceID       domain.SequenceID
	sequenceRevision domain.Revision
	planDigest       domain.Digest
	rendererVersion  string
	rendererTarget   string
	profile          string
	state            domain.SequenceExportArtifactState
	facts            domain.RenderedMediaFacts
	byteReference    string
	byteSize         uint64
	contentDigest    domain.Digest
}

func (repository *SQLiteProjects) ReconcileSequenceExportStorage(ctx context.Context, now time.Time) error {
	if now.IsZero() {
		return application.ErrSequenceExportInvalid
	}
	repository.artifactLifecycleMu.Lock()
	defer repository.artifactLifecycleMu.Unlock()
	if err := reconcileSequenceExportWorkRoot(
		filepath.Join(repository.dataDir, "work", "sequence-export-publication"), true,
	); err != nil {
		return err
	}
	if err := reconcileSequenceExportWorkRoot(
		filepath.Join(repository.dataDir, "work", "sequence-export-attempts"), false,
	); err != nil {
		return err
	}
	artifactRoot := filepath.Join(repository.dataDir, "artifacts", "sequence-export")
	quarantineRoot := filepath.Join(repository.dataDir, "work", "sequence-export-quarantine")
	deletionRoot := filepath.Join(repository.dataDir, "work", "sequence-export-deletions")
	if err := os.MkdirAll(artifactRoot, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(quarantineRoot, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(deletionRoot, 0o700); err != nil {
		return err
	}
	records, err := repository.loadStoredSequenceExportArtifacts(ctx)
	if err != nil {
		return err
	}
	if err := reconcileSequenceExportDeletionRoot(
		deletionRoot, artifactRoot, quarantineRoot, records,
	); err != nil {
		return err
	}
	entries, err := os.ReadDir(artifactRoot)
	if err != nil {
		return err
	}
	changedArtifacts := false
	for _, entry := range entries {
		artifactID, parseErr := domain.ParseArtifactID(entry.Name())
		if parseErr != nil {
			continue
		}
		if record, retained := records[artifactID.String()]; retained && record.state != domain.SequenceExportArtifactDeleted {
			continue
		}
		if err := os.RemoveAll(filepath.Join(artifactRoot, entry.Name())); err != nil {
			return err
		}
		changedArtifacts = true
	}
	if changedArtifacts {
		if err := syncDirectory(artifactRoot); err != nil {
			return err
		}
	}
	for _, record := range records {
		canonicalRoot := filepath.Join(artifactRoot, record.id.String())
		if record.state == domain.SequenceExportArtifactDeleted {
			canonicalExists, err := pathExists(canonicalRoot)
			if err != nil {
				return err
			}
			if canonicalExists {
				if err := os.RemoveAll(canonicalRoot); err != nil {
					return err
				}
				if err := syncDirectory(artifactRoot); err != nil {
					return err
				}
			}
			quarantine := filepath.Join(quarantineRoot, record.id.String())
			quarantineExists, err := pathExists(quarantine)
			if err != nil {
				return err
			}
			if quarantineExists {
				if err := os.RemoveAll(quarantine); err != nil {
					return err
				}
				if err := syncDirectory(quarantineRoot); err != nil {
					return err
				}
			}
			continue
		}
		if record.state == domain.SequenceExportArtifactValid {
			if err := repository.verifyStoredSequenceExportArtifact(record); err == nil {
				continue
			}
			if err := repository.invalidateStoredSequenceExportArtifact(ctx, record, now.UTC()); err != nil {
				return err
			}
		}
		if err := quarantineSequenceExportRoot(canonicalRoot, quarantineRoot, record.id); err != nil {
			return err
		}
	}
	return nil
}

func reconcileSequenceExportWorkRoot(root string, publication bool) error {
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
		owned := false
		if publication {
			owned = len(name) == 73 && name[36] == '-'
			if owned {
				_, attemptErr := domain.ParseJobAttemptID(name[:36])
				_, artifactErr := domain.ParseArtifactID(name[37:])
				owned = attemptErr == nil && artifactErr == nil
			}
		} else {
			_, attemptErr := domain.ParseJobAttemptID(name)
			owned = attemptErr == nil
		}
		if !owned || !entry.IsDir() {
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

func (repository *SQLiteProjects) loadStoredSequenceExportArtifacts(
	ctx context.Context,
) (map[string]storedSequenceExportArtifact, error) {
	rows, err := repository.db.QueryContext(ctx, `
SELECT id, producer_job_id, project_id, sequence_id, sequence_revision,
       render_plan_digest, renderer_version, renderer_target, output_profile,
       state, facts_json, byte_reference, byte_size, content_digest
FROM sequence_export_artifacts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make(map[string]storedSequenceExportArtifact)
	for rows.Next() {
		var (
			idValue, producerValue, projectValue, sequenceValue, planValue string
			stateValue, factsJSON, contentDigestValue                      string
			revisionValue                                                  uint64
			record                                                         storedSequenceExportArtifact
		)
		if err := rows.Scan(
			&idValue, &producerValue, &projectValue, &sequenceValue, &revisionValue,
			&planValue, &record.rendererVersion, &record.rendererTarget, &record.profile,
			&stateValue, &factsJSON, &record.byteReference, &record.byteSize, &contentDigestValue,
		); err != nil {
			return nil, err
		}
		var parseErr error
		record.id, parseErr = domain.ParseArtifactID(idValue)
		if parseErr == nil {
			record.producerJobID, parseErr = domain.ParseWorkJobID(producerValue)
		}
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
		record.state = domain.SequenceExportArtifactState(stateValue)
		if parseErr != nil || (record.state != domain.SequenceExportArtifactValid &&
			record.state != domain.SequenceExportArtifactInvalid &&
			record.state != domain.SequenceExportArtifactDeleted) ||
			record.profile != domain.SequenceExportProfileV1 ||
			json.Unmarshal([]byte(factsJSON), &record.facts) != nil ||
			application.ValidateSequenceExportFacts(record.facts) != nil {
			return nil, application.ErrSequenceExportInvalid
		}
		records[idValue] = record
	}
	return records, rows.Err()
}

func reconcileSequenceExportDeletionRoot(
	deletionRoot string,
	artifactRoot string,
	quarantineRoot string,
	records map[string]storedSequenceExportArtifact,
) error {
	entries, err := os.ReadDir(deletionRoot)
	if err != nil {
		return err
	}
	changed := false
	for _, entry := range entries {
		artifactID, parseErr := domain.ParseArtifactID(entry.Name())
		if parseErr != nil || !entry.IsDir() {
			continue
		}
		stage := filepath.Join(deletionRoot, artifactID.String())
		record, retained := records[artifactID.String()]
		if !retained || record.state == domain.SequenceExportArtifactDeleted {
			if err := os.RemoveAll(stage); err != nil {
				return err
			}
			changed = true
			continue
		}
		target := filepath.Join(quarantineRoot, artifactID.String())
		if record.state == domain.SequenceExportArtifactValid {
			target = filepath.Join(artifactRoot, artifactID.String())
		}
		if exists, err := pathExists(target); err != nil {
			return err
		} else if exists {
			if err := os.RemoveAll(stage); err != nil {
				return err
			}
		} else if err := os.Rename(stage, target); err != nil {
			return err
		}
		if err := syncDirectory(filepath.Dir(target)); err != nil {
			return err
		}
		changed = true
	}
	if changed {
		return syncDirectory(deletionRoot)
	}
	return nil
}

func (repository *SQLiteProjects) verifyStoredSequenceExportArtifact(
	record storedSequenceExportArtifact,
) error {
	file, _, err := repository.openStoredSequenceExportArtifact(record)
	if file != nil {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}

func (repository *SQLiteProjects) inspectStoredSequenceExportArtifact(
	record storedSequenceExportArtifact,
) (application.SequenceExportArtifactManifest, error) {
	if record.state != domain.SequenceExportArtifactValid ||
		record.byteReference != "artifact:sequence-export/"+record.id.String() {
		return application.SequenceExportArtifactManifest{}, application.ErrSequenceExportInvalid
	}
	root := filepath.Join(repository.dataDir, "artifacts", "sequence-export", record.id.String())
	if err := requireDirectory(root); err != nil {
		return application.SequenceExportArtifactManifest{}, err
	}
	if err := requireExactDirectoryEntries(root, []string{"export.webm", "manifest.json"}); err != nil {
		return application.SequenceExportArtifactManifest{}, err
	}
	manifestBytes, err := readBoundedRegularFile(
		filepath.Join(root, "manifest.json"), application.MaximumSequenceExportManifestSize,
	)
	if err != nil {
		return application.SequenceExportArtifactManifest{}, err
	}
	manifestHash := sha256.Sum256(manifestBytes)
	if record.contentDigest.String() != "sha256:"+hex.EncodeToString(manifestHash[:]) {
		return application.SequenceExportArtifactManifest{}, fmt.Errorf("export manifest digest mismatch")
	}
	manifest, err := application.DecodeSequenceExportArtifactManifest(manifestBytes)
	if err != nil || manifest.ProducerJobID != record.producerJobID ||
		manifest.ProjectID != record.projectID || manifest.SequenceID != record.sequenceID ||
		manifest.SequenceRevision != record.sequenceRevision || manifest.RenderPlanDigest != record.planDigest ||
		manifest.RendererVersion != record.rendererVersion || manifest.RendererTarget != record.rendererTarget ||
		manifest.Profile != record.profile || manifest.Facts != record.facts {
		return application.SequenceExportArtifactManifest{}, application.ErrSequenceExportInvalid
	}
	mediaPath := filepath.Join(root, manifest.Media.Path)
	info, err := os.Lstat(mediaPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		uint64(info.Size()) != manifest.Media.ByteSize.Value() ||
		uint64(len(manifestBytes))+manifest.Media.ByteSize.Value() != record.byteSize {
		return application.SequenceExportArtifactManifest{}, fmt.Errorf("export media structure is invalid")
	}
	return manifest, nil
}

func (repository *SQLiteProjects) openStoredSequenceExportArtifact(
	record storedSequenceExportArtifact,
) (*os.File, application.SequenceExportArtifactManifest, error) {
	manifest, err := repository.inspectStoredSequenceExportArtifact(record)
	if err != nil {
		return nil, application.SequenceExportArtifactManifest{}, err
	}
	root := filepath.Join(repository.dataDir, "artifacts", "sequence-export", record.id.String())
	mediaPath := filepath.Join(root, manifest.Media.Path)
	file, err := os.Open(mediaPath)
	if err != nil {
		return nil, application.SequenceExportArtifactManifest{}, err
	}
	failed := true
	defer func() {
		if failed {
			_ = file.Close()
		}
	}()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || uint64(info.Size()) != manifest.Media.ByteSize.Value() {
		return nil, application.SequenceExportArtifactManifest{}, fmt.Errorf("export media identity is invalid")
	}
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(file, int64(manifest.Media.ByteSize.Value())+1))
	if err != nil {
		return nil, application.SequenceExportArtifactManifest{}, err
	}
	if uint64(written) != manifest.Media.ByteSize.Value() ||
		"sha256:"+hex.EncodeToString(digest.Sum(nil)) != manifest.Media.SHA256.String() {
		return nil, application.SequenceExportArtifactManifest{}, fmt.Errorf("export media digest or size mismatch")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, application.SequenceExportArtifactManifest{}, err
	}
	failed = false
	return file, manifest, nil
}

func (repository *SQLiteProjects) invalidateStoredSequenceExportArtifact(
	ctx context.Context,
	record storedSequenceExportArtifact,
	at time.Time,
) error {
	eventValue, err := domain.GenerateUUIDv7(at)
	if err != nil {
		return err
	}
	eventID, err := domain.ParseActivityEventID(eventValue)
	if err != nil {
		return err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
UPDATE sequence_export_artifacts SET state = 'invalid' WHERE id = ? AND state = 'valid'`,
		record.id.String())
	if err != nil {
		return err
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return application.ErrSequenceExportInvalid
	}
	var projectRevision uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id = ?`,
		record.projectID.String()).Scan(&projectRevision); err != nil {
		return err
	}
	revision, err := domain.NewRevision(projectRevision)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		ChangedEntityRefs []application.ChangedEntityRef `json:"changedEntityRefs"`
		ArtifactID        domain.ArtifactID              `json:"artifactId"`
		JobID             domain.WorkJobID               `json:"jobId"`
		SequenceID        domain.SequenceID              `json:"sequenceId"`
		SequenceRevision  domain.Revision                `json:"sequenceRevision"`
	}{
		ChangedEntityRefs: []application.ChangedEntityRef{}, ArtifactID: record.id,
		JobID: record.producerJobID, SequenceID: record.sequenceID,
		SequenceRevision: record.sequenceRevision,
	})
	if err != nil {
		return err
	}
	if _, err := appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: record.projectID.String(), EventID: eventID.String(),
		Kind: "sequence.export-invalidated", OccurredAt: formatInstant(at),
		ProjectID: record.projectID.String(), ProjectRevision: int64(revision.Value()),
		OutcomeKind: "export-artifact", OutcomeID: record.id.String(),
		SummaryCode: "sequence-export-invalidated", Payload: payload,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func quarantineSequenceExportRoot(
	canonicalRoot string,
	quarantineRoot string,
	artifactID domain.ArtifactID,
) error {
	if _, err := os.Lstat(canonicalRoot); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	destination := filepath.Join(quarantineRoot, artifactID.String())
	if _, err := os.Lstat(destination); err == nil {
		if err := os.RemoveAll(canonicalRoot); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else if err := os.Rename(canonicalRoot, destination); err != nil {
		return err
	}
	if err := syncDirectory(filepath.Dir(canonicalRoot)); err != nil {
		return err
	}
	return syncDirectory(quarantineRoot)
}
