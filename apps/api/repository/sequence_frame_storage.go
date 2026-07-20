package repository

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type storedSequenceFrameArtifact struct {
	id                    domain.ArtifactID
	producerJobID         domain.WorkJobID
	projectID             domain.ProjectID
	sequenceID            domain.SequenceID
	sequenceRevision      domain.Revision
	previewJobID          domain.WorkJobID
	previewArtifactID     domain.ArtifactID
	previewArtifactDigest domain.Digest
	renderPlanDigest      domain.Digest
	parameters            application.SequenceFrameSetParameters
	parametersJSON        []byte
	parametersDigest      domain.Digest
	producerVersion       string
	profile               string
	gridPolicy            string
	byteReference         string
	byteSize              uint64
	contentDigest         domain.Digest
}

func (repository *SQLiteProjects) ReconcileSequenceFrameStorage(
	ctx context.Context,
	now time.Time,
) error {
	if now.IsZero() {
		return application.ErrProductStorageInvalid
	}
	repository.artifactLifecycleMu.Lock()
	defer repository.artifactLifecycleMu.Unlock()
	if err := repository.reconcileSequenceFrameEvictionWork(ctx); err != nil {
		return err
	}
	if err := repository.reconcileSequenceFramePublicationWork(); err != nil {
		return err
	}
	if err := repository.reconcileSequenceFrameAttemptWork(); err != nil {
		return err
	}
	root := filepath.Join(repository.dataDir, "artifacts", "sequence-frames")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	records, err := repository.loadStoredSequenceFrameArtifacts(ctx)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
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
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
		removed = true
	}
	if removed {
		if err := syncDirectory(root); err != nil {
			return err
		}
	}
	for _, record := range records {
		if err := repository.verifyStoredSequenceFrameArtifact(record); err != nil {
			if evictErr := repository.evictCorruptSequenceFrameArtifact(ctx, record, now.UTC(), err); evictErr != nil {
				return fmt.Errorf("evict corrupt sequence frame artifact %s: %w", record.id, evictErr)
			}
		}
	}
	return nil
}

func (repository *SQLiteProjects) loadStoredSequenceFrameArtifacts(
	ctx context.Context,
) (map[string]storedSequenceFrameArtifact, error) {
	rows, err := repository.db.QueryContext(ctx, `
SELECT artifact.id, artifact.producer_job_id, artifact.project_id, artifact.sequence_id,
       artifact.sequence_revision, artifact.preview_job_id, artifact.preview_artifact_id,
       artifact.preview_artifact_digest, artifact.render_plan_digest,
       artifact.parameters_digest, job.parameters_json, job.parameters_digest,
       artifact.producer_version, artifact.profile, artifact.grid_policy,
       artifact.byte_reference, artifact.byte_size, artifact.content_digest
FROM sequence_frame_set_artifacts artifact
JOIN work_jobs job ON job.id = artifact.producer_job_id
WHERE artifact.state = 'ready'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]storedSequenceFrameArtifact)
	for rows.Next() {
		var (
			idValue, producerJobValue, projectValue, sequenceValue, previewJobValue string
			previewArtifactValue, previewDigestValue, planDigestValue               string
			parametersDigestValue, jobParametersDigestValue, parametersJSON         string
			contentDigestValue                                                      string
			revisionValue                                                           uint64
			record                                                                  storedSequenceFrameArtifact
		)
		if err := rows.Scan(
			&idValue, &producerJobValue, &projectValue, &sequenceValue, &revisionValue,
			&previewJobValue, &previewArtifactValue, &previewDigestValue, &planDigestValue,
			&parametersDigestValue, &parametersJSON, &jobParametersDigestValue,
			&record.producerVersion, &record.profile, &record.gridPolicy,
			&record.byteReference, &record.byteSize, &contentDigestValue,
		); err != nil {
			return nil, err
		}
		var parseErr error
		record.id, parseErr = domain.ParseArtifactID(idValue)
		if parseErr == nil {
			record.producerJobID, parseErr = domain.ParseWorkJobID(producerJobValue)
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
			record.previewJobID, parseErr = domain.ParseWorkJobID(previewJobValue)
		}
		if parseErr == nil {
			record.previewArtifactID, parseErr = domain.ParseArtifactID(previewArtifactValue)
		}
		if parseErr == nil {
			record.previewArtifactDigest, parseErr = domain.ParseDigest(previewDigestValue)
		}
		if parseErr == nil {
			record.renderPlanDigest, parseErr = domain.ParseDigest(planDigestValue)
		}
		if parseErr == nil {
			record.parametersDigest, parseErr = domain.ParseDigest(parametersDigestValue)
		}
		if parseErr == nil {
			record.contentDigest, parseErr = domain.ParseDigest(contentDigestValue)
		}
		if parseErr != nil || parametersDigestValue != jobParametersDigestValue {
			return nil, application.ErrSequenceFramesInvalid
		}
		record.parametersJSON = []byte(parametersJSON)
		record.parameters, parseErr = application.DecodeSequenceFrameSetParameters(record.parametersJSON)
		if parseErr != nil {
			return nil, application.ErrSequenceFramesInvalid
		}
		result[idValue] = record
	}
	return result, rows.Err()
}

func (repository *SQLiteProjects) verifyStoredSequenceFrameArtifact(
	record storedSequenceFrameArtifact,
) error {
	canonicalParameters, parametersDigest, err := application.CanonicalSequenceFrameSetParameters(record.parameters)
	if err != nil || !bytes.Equal(canonicalParameters, record.parametersJSON) ||
		parametersDigest != record.parametersDigest || record.parameters.ProjectID != record.projectID ||
		record.parameters.SequenceID != record.sequenceID ||
		record.parameters.SequenceRevision != record.sequenceRevision ||
		record.parameters.PreviewJobID != record.previewJobID ||
		record.parameters.ExecutorVersion != record.producerVersion ||
		record.parameters.Profile != record.profile || record.parameters.GridPolicy != record.gridPolicy {
		return application.ErrSequenceFramesInvalid
	}
	if record.byteReference != "artifact:sequence-frames/"+record.id.String() {
		return fmt.Errorf("unexpected byte reference")
	}
	root := filepath.Join(repository.dataDir, "artifacts", "sequence-frames", record.id.String())
	if err := requireDirectory(root); err != nil {
		return err
	}
	if err := requireExactDirectoryEntries(root, []string{"frames", "manifest.json"}); err != nil {
		return err
	}
	manifestBytes, err := readBoundedRegularFile(
		filepath.Join(root, "manifest.json"), application.MaximumSequenceFrameArtifactSize,
	)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(manifestBytes)
	if record.contentDigest.String() != "sha256:"+hex.EncodeToString(hash[:]) {
		return fmt.Errorf("manifest digest mismatch")
	}
	manifest, err := application.DecodeSequenceFrameArtifactManifest(manifestBytes)
	if err != nil || manifest.ProjectID != record.projectID || manifest.SequenceID != record.sequenceID ||
		manifest.SequenceRevision != record.sequenceRevision || manifest.PreviewJobID != record.previewJobID ||
		manifest.PreviewArtifactID != record.previewArtifactID ||
		manifest.PreviewArtifactDigest != record.previewArtifactDigest ||
		manifest.RenderPlanDigest != record.renderPlanDigest || manifest.Producer != record.producerVersion ||
		manifest.Profile != record.profile || manifest.GridPolicy != record.gridPolicy ||
		len(manifest.Samples) != len(record.parameters.Samples) {
		return application.ErrSequenceFramesInvalid
	}
	framesRoot := filepath.Join(root, "frames")
	if err := requireDirectory(framesRoot); err != nil {
		return err
	}
	expected := make([]string, len(manifest.Samples))
	total := uint64(len(manifestBytes))
	for index, sample := range manifest.Samples {
		if sample.SequenceFrameCoordinate != record.parameters.Samples[index] {
			return application.ErrSequenceFramesInvalid
		}
		expected[index] = fmt.Sprintf("%03d.png", index)
		data, err := readBoundedRegularFile(
			filepath.Join(root, filepath.FromSlash(sample.Path)), application.MaximumSequenceFrameArtifactSize,
		)
		if err != nil || uint64(len(data)) != sample.ByteSize.Value() {
			return fmt.Errorf("sequence frame sample is invalid")
		}
		digest := sha256.Sum256(data)
		if sample.SHA256.String() != "sha256:"+hex.EncodeToString(digest[:]) {
			return fmt.Errorf("sequence frame sample digest mismatch")
		}
		reader := bytes.NewReader(data)
		decoded, decodeErr := png.Decode(reader)
		if decodeErr != nil || reader.Len() != 0 || decoded.Bounds().Dx() != int(sample.Width) ||
			decoded.Bounds().Dy() != int(sample.Height) || !opaqueSequenceFrame(decoded) {
			return fmt.Errorf("sequence frame PNG is invalid")
		}
		total += uint64(len(data))
	}
	if err := requireExactDirectoryEntries(framesRoot, expected); err != nil {
		return err
	}
	if total != record.byteSize {
		return fmt.Errorf("sequence frame artifact aggregate size mismatch")
	}
	return nil
}

func (repository *SQLiteProjects) evictCorruptSequenceFrameArtifact(
	ctx context.Context,
	record storedSequenceFrameArtifact,
	now time.Time,
	cause error,
) error {
	eventValue, err := domain.GenerateUUIDv7From(now.UTC(), rand.Reader)
	if err != nil {
		return err
	}
	eventID, err := domain.ParseActivityEventID(eventValue)
	if err != nil {
		return err
	}
	artifactRoot := filepath.Join(repository.dataDir, "artifacts", "sequence-frames")
	canonicalRoot := filepath.Join(artifactRoot, record.id.String())
	evictionRoot := filepath.Join(repository.dataDir, "work", "sequence-frame-evictions")
	eventRoot := filepath.Join(evictionRoot, eventID.String())
	quarantineRoot := filepath.Join(eventRoot, record.id.String())
	quarantined := false
	if _, statErr := os.Lstat(canonicalRoot); statErr == nil {
		if err := os.MkdirAll(eventRoot, 0o700); err != nil {
			return err
		}
		if err := os.Rename(canonicalRoot, quarantineRoot); err != nil {
			return err
		}
		quarantined = true
		if err := syncDirectory(artifactRoot); err != nil {
			return repository.restoreSequenceFrameEviction(canonicalRoot, eventRoot, quarantineRoot, err)
		}
		// The quarantine directory must be durable too: without this the rename
		// can be visible in the artifact root while the entry it moved to is not.
		if err := syncDirectory(eventRoot); err != nil {
			return repository.restoreSequenceFrameEviction(canonicalRoot, eventRoot, quarantineRoot, err)
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		if quarantined {
			return repository.restoreSequenceFrameEviction(canonicalRoot, eventRoot, quarantineRoot, err)
		}
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
UPDATE sequence_frame_set_artifacts SET state = 'evicted'
WHERE id = ? AND producer_job_id = ? AND state = 'ready'`,
		record.id.String(), record.producerJobID.String(),
	)
	if err != nil {
		return err
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return application.ErrSequenceFramesInvalid
	}
	if _, err := tx.ExecContext(ctx, `
DELETE FROM sequence_frame_scratch_leases WHERE artifact_id = ?`, record.id.String()); err != nil {
		return err
	}
	if err := appendSequenceFrameEvictedActivity(ctx, tx, record, eventID, now, cause); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		// A failed commit is ambiguous. Restoring the canonical tree would put
		// back bytes for an artifact SQLite may already consider evicted; leave
		// the recognized quarantine work for startup reconciliation instead.
		return err
	}
	if quarantined {
		if err := os.RemoveAll(eventRoot); err != nil {
			return err
		}
		return syncDirectory(evictionRoot)
	}
	return nil
}

func appendSequenceFrameEvictedActivity(
	ctx context.Context,
	tx *sql.Tx,
	record storedSequenceFrameArtifact,
	eventID domain.ActivityEventID,
	now time.Time,
	cause error,
) error {
	var projectRevision uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id = ?`,
		record.projectID.String(),
	).Scan(&projectRevision); err != nil {
		return err
	}
	diagnostic := "sequence-frame-artifact-corrupt"
	if cause != nil && strings.Contains(cause.Error(), "unavailable") {
		diagnostic = "sequence-frame-artifact-unavailable"
	}
	payload, err := json.Marshal(struct {
		ChangedEntityRefs []application.ChangedEntityRef `json:"changedEntityRefs"`
		ArtifactID        domain.ArtifactID              `json:"artifactId"`
		JobID             domain.WorkJobID               `json:"jobId"`
		FailureCode       string                         `json:"failureCode"`
	}{
		ChangedEntityRefs: []application.ChangedEntityRef{}, ArtifactID: record.id,
		JobID: record.producerJobID, FailureCode: diagnostic,
	})
	if err != nil {
		return err
	}
	_, err = appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: record.projectID.String(), EventID: eventID.String(),
		Kind: "sequence.frame-set-artifact-evicted", OccurredAt: formatInstant(now.UTC()),
		ProjectID: record.projectID.String(), ProjectRevision: int64(projectRevision),
		OutcomeKind: "sequence-frame-artifact", OutcomeID: record.id.String(),
		SummaryCode: diagnostic, Payload: payload,
	})
	return err
}

func (repository *SQLiteProjects) restoreSequenceFrameEviction(
	canonicalRoot string,
	eventRoot string,
	quarantineRoot string,
	cause error,
) error {
	if _, err := os.Lstat(canonicalRoot); !os.IsNotExist(err) {
		return fmt.Errorf("evict sequence frame artifact: %w; restore destination is not empty", cause)
	}
	if err := os.Rename(quarantineRoot, canonicalRoot); err != nil {
		return fmt.Errorf("evict sequence frame artifact: %w; restore: %v", cause, err)
	}
	_ = os.Remove(eventRoot)
	_ = syncDirectory(filepath.Dir(canonicalRoot))
	return cause
}

func (repository *SQLiteProjects) reconcileSequenceFrameEvictionWork(ctx context.Context) error {
	root := filepath.Join(repository.dataDir, "work", "sequence-frame-evictions")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	artifactRoot := filepath.Join(repository.dataDir, "artifacts", "sequence-frames")
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
SELECT state FROM sequence_frame_set_artifacts WHERE id = ?`, artifactID.String()).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		quarantineRoot := filepath.Join(eventRoot, artifactID.String())
		canonicalRoot := filepath.Join(artifactRoot, artifactID.String())
		switch state {
		case "ready":
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
		case "evicted":
			if err := os.RemoveAll(eventRoot); err != nil {
				return err
			}
		default:
			return application.ErrSequenceFramesInvalid
		}
	}
	return syncDirectory(root)
}

func (repository *SQLiteProjects) reconcileSequenceFramePublicationWork() error {
	root := filepath.Join(repository.dataDir, "work", "sequence-frame-publication")
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

func (repository *SQLiteProjects) reconcileSequenceFrameAttemptWork() error {
	root := filepath.Join(repository.dataDir, "work", "sequence-frame-attempts")
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
