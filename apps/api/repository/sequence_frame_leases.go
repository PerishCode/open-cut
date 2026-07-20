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
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) MaterializeSequenceFrameLeases(
	ctx context.Context,
	record application.MaterializeSequenceFrameLeasesRecord,
) (leases []application.SequenceFrameResourceLease, resultErr error) {
	if err := validateSequenceFrameLeaseRecord(record); err != nil {
		return nil, err
	}
	if err := repository.ReconcileProductScratchLeases(ctx, record.CreatedAt.UTC()); err != nil {
		return nil, err
	}
	if existing, found, err := repository.loadReusableSequenceFrameLeases(ctx, record); err != nil {
		return nil, err
	} else if found {
		return existing, nil
	}
	contentDigest, aggregateSize, err := repository.verifySequenceFrameLeaseAuthority(ctx, record)
	if err != nil {
		return nil, err
	}
	artifactRoot := filepath.Join(repository.dataDir, "artifacts", "sequence-frames", record.ArtifactID.String())
	manifestBytes, err := readBoundedRegularFile(
		filepath.Join(artifactRoot, "manifest.json"), application.MaximumSequenceFrameArtifactSize,
	)
	if err != nil {
		return nil, err
	}
	manifestHash := sha256.Sum256(manifestBytes)
	if contentDigest != "sha256:"+hex.EncodeToString(manifestHash[:]) {
		return nil, fmt.Errorf("sequence frame artifact manifest digest mismatch")
	}
	manifest, err := application.DecodeSequenceFrameArtifactManifest(manifestBytes)
	if err != nil || manifest.ProjectID != record.ProjectID || manifest.SequenceID != record.SequenceID ||
		manifest.SequenceRevision != record.SequenceRevision || len(manifest.Samples) != len(record.Resources) {
		return nil, application.ErrSequenceFramesInvalid
	}
	stageParent := filepath.Join(repository.dataDir, "work", "scratch-publication")
	if err := os.MkdirAll(stageParent, 0o700); err != nil {
		return nil, err
	}
	stageRoot := filepath.Join(stageParent, record.LeaseSetID.String())
	if err := os.Mkdir(stageRoot, 0o700); err != nil {
		return nil, err
	}
	defer os.RemoveAll(stageRoot)
	result := make([]application.SequenceFrameResourceLease, 0, len(record.Resources))
	total := uint64(len(manifestBytes))
	for index, sample := range manifest.Samples {
		pngBytes, readErr := readBoundedRegularFile(
			filepath.Join(artifactRoot, filepath.FromSlash(sample.Path)),
			application.MaximumSequenceFrameArtifactSize,
		)
		if readErr != nil {
			return nil, readErr
		}
		digest := sha256.Sum256(pngBytes)
		if uint64(len(pngBytes)) != sample.ByteSize.Value() ||
			"sha256:"+hex.EncodeToString(digest[:]) != sample.SHA256.String() {
			return nil, fmt.Errorf("sequence frame artifact sample digest mismatch")
		}
		total += uint64(len(pngBytes))
		destination := filepath.Join(stageRoot, record.Resources[index].String()+".png")
		if err := writeDurableArtifactFile(destination, pngBytes); err != nil {
			return nil, err
		}
		if err := os.Chmod(destination, 0o400); err != nil {
			return nil, err
		}
		result = append(result, application.SequenceFrameResourceLease{
			ResourceID: record.Resources[index], MIMEType: "image/png", ByteSize: sample.ByteSize,
			SHA256: sample.SHA256, RequestedTime: sample.RequestedTime,
			SequenceTime: sample.SequenceTime, FrameIndex: sample.FrameIndex,
			ExpiresAt: record.ExpiresAt.UTC(),
		})
	}
	if total != aggregateSize {
		return nil, fmt.Errorf("sequence frame artifact aggregate size mismatch")
	}
	if err := syncDirectory(stageRoot); err != nil {
		return nil, err
	}
	turnRoot := filepath.Join(
		repository.dataDir, "scratch", "runs", record.RunID.String(), "turns", record.TurnID.String(),
	)
	if err := os.MkdirAll(turnRoot, 0o700); err != nil {
		return nil, err
	}
	finalRoot := filepath.Join(turnRoot, record.LeaseSetID.String())
	if _, err := os.Lstat(finalRoot); !os.IsNotExist(err) {
		return nil, fmt.Errorf("sequence frame lease destination already exists")
	}
	if err := os.Rename(stageRoot, finalRoot); err != nil {
		return nil, err
	}
	if err := syncDirectory(turnRoot); err != nil {
		return nil, err
	}
	for index := range result {
		result[index].ReadOnlyPath = filepath.Join(finalRoot, result[index].ResourceID.String()+".png")
	}
	defer func() { discardUnpublishedTree(finalRoot, &resultErr) }()
	if err := repository.commitSequenceFrameLeases(ctx, record, result); err != nil {
		return nil, err
	}
	return result, nil
}

func validateSequenceFrameLeaseRecord(record application.MaterializeSequenceFrameLeasesRecord) error {
	if record.ProjectID.IsZero() || record.SequenceID.IsZero() || record.RunID.IsZero() ||
		record.TurnID.IsZero() || record.JobID.IsZero() || record.ArtifactID.IsZero() ||
		record.SequenceRevision.Value() == 0 || record.LeaseSetID.IsZero() ||
		record.Actor.Validate() != nil || record.Actor.Kind != domain.ActorAgent ||
		len(record.Resources) == 0 || len(record.Resources) > application.MaximumSequenceFrameSamples ||
		record.CreatedAt.IsZero() || !record.ExpiresAt.Equal(record.CreatedAt.Add(5*time.Minute)) {
		return application.ErrSequenceFramesInvalid
	}
	seen := map[string]struct{}{record.LeaseSetID.String(): {}}
	for _, resource := range record.Resources {
		if resource.IsZero() {
			return application.ErrSequenceFramesInvalid
		}
		if _, duplicate := seen[resource.String()]; duplicate {
			return application.ErrSequenceFramesInvalid
		}
		seen[resource.String()] = struct{}{}
	}
	return nil
}

func (repository *SQLiteProjects) verifySequenceFrameLeaseAuthority(
	ctx context.Context,
	record application.MaterializeSequenceFrameLeasesRecord,
) (string, uint64, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()
	if err := verifyActiveSequenceFrameTurn(ctx, tx, record.ReadSequenceFrameSetRecord, true); err != nil {
		return "", 0, err
	}
	digest, size, err := verifySequenceFrameLeaseArtifact(ctx, tx, record)
	if err != nil {
		return "", 0, err
	}
	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	return digest, size, nil
}

func verifySequenceFrameLeaseArtifact(
	ctx context.Context,
	tx *sql.Tx,
	record application.MaterializeSequenceFrameLeasesRecord,
) (string, uint64, error) {
	var digest string
	var size uint64
	err := tx.QueryRowContext(ctx, `
SELECT artifact.content_digest, artifact.byte_size
FROM work_jobs job
JOIN sequence_frame_set_job_details detail ON detail.job_id = job.id
JOIN sequence_frame_set_artifacts artifact ON artifact.id = detail.result_artifact_id
WHERE job.id = ? AND job.project_id = ? AND detail.sequence_id = ?
  AND job.kind = 'sequence-frame-set' AND job.state = 'succeeded'
  AND artifact.id = ? AND artifact.producer_job_id = job.id AND artifact.state = 'ready'`,
		record.JobID.String(), record.ProjectID.String(), record.SequenceID.String(), record.ArtifactID.String(),
	).Scan(&digest, &size)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, application.ErrSequenceFramesInvalid
	}
	return digest, size, err
}

func (repository *SQLiteProjects) commitSequenceFrameLeases(
	ctx context.Context,
	record application.MaterializeSequenceFrameLeasesRecord,
	resources []application.SequenceFrameResourceLease,
) error {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifyActiveSequenceFrameTurn(ctx, tx, record.ReadSequenceFrameSetRecord, true); err != nil {
		return err
	}
	if _, _, err := verifySequenceFrameLeaseArtifact(ctx, tx, record); err != nil {
		return err
	}
	for index, resource := range resources {
		requestedJSON, err := json.Marshal(resource.RequestedTime)
		if err != nil {
			return err
		}
		sequenceJSON, err := json.Marshal(resource.SequenceTime)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(repository.dataDir, resource.ReadOnlyPath)
		if err != nil || !validScratchRelative(relative) {
			return application.ErrSequenceFramesInvalid
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_frame_scratch_leases (
  resource_id, lease_set_id, project_id, sequence_id, sequence_revision,
  run_id, turn_id, job_id, artifact_id, sample_index, frame_index,
  relative_path, mime_type, byte_size, sha256, requested_time_json,
  sequence_time_json, expires_at, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'image/png', ?, ?, ?, ?, ?, ?)`,
			resource.ResourceID.String(), record.LeaseSetID.String(), record.ProjectID.String(),
			record.SequenceID.String(), record.SequenceRevision.Value(),
			record.RunID.String(), record.TurnID.String(), record.JobID.String(), record.ArtifactID.String(),
			index, resource.FrameIndex.Value(), filepath.ToSlash(relative), resource.ByteSize.Value(),
			resource.SHA256.String(), string(requestedJSON), string(sequenceJSON),
			formatInstant(record.ExpiresAt.UTC()), formatInstant(record.CreatedAt.UTC()),
		); err != nil {
			return err
		}
	}
	return commitPublication(tx)
}

func (repository *SQLiteProjects) loadReusableSequenceFrameLeases(
	ctx context.Context,
	record application.MaterializeSequenceFrameLeasesRecord,
) ([]application.SequenceFrameResourceLease, bool, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if err := verifyActiveSequenceFrameTurn(ctx, tx, record.ReadSequenceFrameSetRecord, true); err != nil {
		return nil, false, err
	}
	rows, err := tx.QueryContext(ctx, `
SELECT resource_id, relative_path, byte_size, sha256, requested_time_json,
       sequence_time_json, frame_index, expires_at
FROM sequence_frame_scratch_leases
WHERE run_id = ? AND turn_id = ? AND job_id = ? AND artifact_id = ?
  AND expires_at > ?
ORDER BY sample_index`, record.RunID.String(), record.TurnID.String(), record.JobID.String(),
		record.ArtifactID.String(), formatInstant(record.CreatedAt.UTC()))
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	result := make([]application.SequenceFrameResourceLease, 0, len(record.Resources))
	for rows.Next() {
		var idValue, relative, digestValue, requestedJSON, sequenceJSON, expiryValue string
		var byteSize, frameIndex uint64
		if err := rows.Scan(
			&idValue, &relative, &byteSize, &digestValue, &requestedJSON,
			&sequenceJSON, &frameIndex, &expiryValue,
		); err != nil {
			return nil, false, err
		}
		resourceID, idErr := domain.ParseResourceID(idValue)
		size, sizeErr := domain.NewUInt64(byteSize)
		digest, digestErr := domain.ParseDigest(digestValue)
		index, indexErr := domain.NewUInt64(frameIndex)
		expiresAt, expiryErr := time.Parse(time.RFC3339Nano, expiryValue)
		var requested, sequence domain.RationalTime
		requestedErr := json.Unmarshal([]byte(requestedJSON), &requested)
		sequenceErr := json.Unmarshal([]byte(sequenceJSON), &sequence)
		if idErr != nil || sizeErr != nil || digestErr != nil || indexErr != nil || expiryErr != nil ||
			requestedErr != nil || sequenceErr != nil || requested.Validate() != nil || sequence.Validate() != nil ||
			!validScratchRelative(filepath.FromSlash(relative)) {
			return nil, false, application.ErrSequenceFramesInvalid
		}
		result = append(result, application.SequenceFrameResourceLease{
			ResourceID: resourceID, MIMEType: "image/png", ByteSize: size, SHA256: digest,
			RequestedTime: requested, SequenceTime: sequence, FrameIndex: index,
			ReadOnlyPath: filepath.Join(repository.dataDir, filepath.FromSlash(relative)),
			ExpiresAt:    expiresAt.UTC(),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(result) == 0 {
		return nil, false, nil
	}
	if len(result) != len(record.Resources) {
		return nil, false, application.ErrSequenceFramesInvalid
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return result, true, nil
}
