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
	"strings"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) MaterializeMediaFrameLeases(
	ctx context.Context,
	record application.MaterializeMediaFrameLeasesRecord,
) (leases []application.FrameResourceLease, resultErr error) {
	if err := repository.ReconcileMediaScratchLeases(ctx, record.CreatedAt.UTC()); err != nil {
		return nil, err
	}
	if err := validateFrameLeaseRecord(record); err != nil {
		return nil, application.ErrAssetInvalid
	}
	contentDigest, aggregateSize, err := repository.verifyFrameLeaseAuthority(ctx, record)
	if err != nil {
		return nil, err
	}
	artifactRoot := filepath.Join(repository.dataDir, "artifacts", "media", record.ArtifactID.String())
	manifestPath := filepath.Join(artifactRoot, "manifest.json")
	manifestBytes, err := readBoundedRegularFile(manifestPath, application.MaximumFrameSetArtifactSize)
	if err != nil {
		return nil, err
	}
	manifestHash := sha256.Sum256(manifestBytes)
	if "sha256:"+hex.EncodeToString(manifestHash[:]) != contentDigest {
		return nil, fmt.Errorf("frame artifact manifest digest mismatch")
	}
	manifest, err := application.DecodeFrameSetArtifactManifest(manifestBytes)
	if err != nil || manifest.AssetID != record.AssetID || len(manifest.Samples) != len(record.Resources) {
		return nil, application.ErrAssetInvalid
	}
	stageParent := filepath.Join(repository.dataDir, "work", "scratch-publication")
	if err := os.MkdirAll(stageParent, 0o700); err != nil {
		return nil, err
	}
	batchName := record.Resources[0].String()
	stageRoot := filepath.Join(stageParent, batchName)
	if err := os.Mkdir(stageRoot, 0o700); err != nil {
		return nil, err
	}
	defer os.RemoveAll(stageRoot)
	result := make([]application.FrameResourceLease, 0, len(record.Resources))
	total := uint64(len(manifestBytes))
	for index, sample := range manifest.Samples {
		source := filepath.Join(artifactRoot, filepath.FromSlash(sample.Path))
		pngBytes, readErr := readBoundedRegularFile(source, application.MaximumFrameSetArtifactSize)
		if readErr != nil {
			return nil, readErr
		}
		digest := sha256.Sum256(pngBytes)
		if uint64(len(pngBytes)) != sample.ByteSize.Value() ||
			"sha256:"+hex.EncodeToString(digest[:]) != sample.SHA256.String() {
			return nil, fmt.Errorf("frame artifact sample digest mismatch")
		}
		total += uint64(len(pngBytes))
		destination := filepath.Join(stageRoot, record.Resources[index].String()+".png")
		if err := writeDurableArtifactFile(destination, pngBytes); err != nil {
			return nil, err
		}
		if err := os.Chmod(destination, 0o400); err != nil {
			return nil, err
		}
		result = append(result, application.FrameResourceLease{
			ResourceID: record.Resources[index], MIMEType: "image/png", ByteSize: sample.ByteSize,
			SHA256: sample.SHA256, RequestedTime: sample.RequestedTime, SourceTime: sample.SourceTime,
			ExpiresAt: record.ExpiresAt.UTC(),
		})
	}
	if total != aggregateSize {
		return nil, fmt.Errorf("frame artifact aggregate size mismatch")
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
	finalRoot := filepath.Join(turnRoot, batchName)
	if _, err := os.Lstat(finalRoot); !os.IsNotExist(err) {
		return nil, fmt.Errorf("frame lease destination already exists")
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
	if err := repository.commitFrameLeases(ctx, record, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (repository *SQLiteProjects) verifyFrameLeaseAuthority(
	ctx context.Context,
	record application.MaterializeMediaFrameLeasesRecord,
) (string, uint64, error) {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()
	if err := verifyActiveFrameTurn(ctx, tx, record); err != nil {
		return "", 0, err
	}
	var digest string
	var size uint64
	err = tx.QueryRowContext(ctx, `
SELECT artifact.content_digest, artifact.byte_size
FROM work_jobs job
JOIN media_job_details detail ON detail.job_id = job.id
JOIN media_artifacts artifact ON artifact.id = detail.result_artifact_id
WHERE job.id = ? AND job.project_id = ? AND detail.asset_id = ?
  AND job.kind = 'frame-sample-set' AND job.state = 'succeeded'
  AND artifact.id = ? AND artifact.kind = 'frame-sample-set' AND artifact.state = 'ready'`,
		record.JobID.String(), record.ProjectID.String(), record.AssetID.String(), record.ArtifactID.String(),
	).Scan(&digest, &size)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, application.ErrAssetInvalid
	}
	if err != nil {
		return "", 0, err
	}
	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	return digest, size, nil
}

func (repository *SQLiteProjects) commitFrameLeases(
	ctx context.Context,
	record application.MaterializeMediaFrameLeasesRecord,
	resources []application.FrameResourceLease,
) error {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifyActiveFrameTurn(ctx, tx, record); err != nil {
		return err
	}
	for index, resource := range resources {
		requestedJSON, err := json.Marshal(resource.RequestedTime)
		if err != nil {
			return err
		}
		sourceJSON, err := json.Marshal(resource.SourceTime)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(repository.dataDir, resource.ReadOnlyPath)
		if err != nil || !validScratchRelative(relative) {
			return application.ErrAssetInvalid
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO media_scratch_leases (
  resource_id, project_id, asset_id, run_id, turn_id, artifact_id, sample_index,
  relative_path, mime_type, byte_size, sha256, requested_time_json, source_time_json,
  expires_at, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'image/png', ?, ?, ?, ?, ?, ?)`,
			resource.ResourceID.String(), record.ProjectID.String(), record.AssetID.String(),
			record.RunID.String(), record.TurnID.String(), record.ArtifactID.String(), index,
			filepath.ToSlash(relative), resource.ByteSize.Value(), resource.SHA256.String(),
			string(requestedJSON), string(sourceJSON), formatInstant(record.ExpiresAt.UTC()),
			formatInstant(record.CreatedAt.UTC()),
		); err != nil {
			return err
		}
	}
	return commitPublication(tx)
}

func verifyActiveFrameTurn(
	ctx context.Context,
	tx *sql.Tx,
	record application.MaterializeMediaFrameLeasesRecord,
) error {
	var valid int
	err := tx.QueryRowContext(ctx, `
SELECT 1
FROM agent_runs run
JOIN agent_turns turn ON turn.id = run.current_turn_id AND turn.run_id = run.id
WHERE run.id = ? AND run.project_id = ? AND run.actor_id = ?
  AND run.status IN ('active', 'waiting')
  AND turn.id = ? AND turn.status = 'active'`,
		record.RunID.String(), record.ProjectID.String(), record.Actor.IDString(), record.TurnID.String(),
	).Scan(&valid)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrRunStaleTurn
	}
	return err
}

func validateFrameLeaseRecord(record application.MaterializeMediaFrameLeasesRecord) error {
	if record.ProjectID.IsZero() || record.AssetID.IsZero() || record.RunID.IsZero() ||
		record.TurnID.IsZero() || record.JobID.IsZero() || record.ArtifactID.IsZero() ||
		record.Actor.Validate() != nil || record.Actor.Kind != domain.ActorAgent ||
		len(record.Resources) == 0 || len(record.Resources) > application.MaximumFrameSetSamples ||
		record.CreatedAt.IsZero() || !record.ExpiresAt.Equal(record.CreatedAt.Add(5*time.Minute)) {
		return application.ErrAssetInvalid
	}
	seen := make(map[string]struct{}, len(record.Resources))
	for _, resource := range record.Resources {
		if resource.IsZero() {
			return application.ErrAssetInvalid
		}
		if _, duplicate := seen[resource.String()]; duplicate {
			return application.ErrAssetInvalid
		}
		seen[resource.String()] = struct{}{}
	}
	return nil
}

func readBoundedRegularFile(path string, maximum int) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() <= 0 || info.Size() > int64(maximum) {
		return nil, fmt.Errorf("artifact file is invalid")
	}
	return os.ReadFile(path)
}

func validScratchRelative(value string) bool {
	clean := filepath.Clean(value)
	return value == clean && !filepath.IsAbs(value) && clean != ".." &&
		!filepath.IsAbs(clean) && len(clean) <= 512 &&
		!stringsHasDotDotPrefix(clean)
}

func stringsHasDotDotPrefix(value string) bool {
	return len(value) > 3 && value[:3] == ".."+string(filepath.Separator)
}

func (repository *SQLiteProjects) revokeTurnScratchLeases(
	ctx context.Context,
	tx *sql.Tx,
	runID domain.RunID,
	turnID domain.TurnID,
) error {
	rows, err := tx.QueryContext(ctx, `
SELECT relative_path FROM media_scratch_leases WHERE run_id = ? AND turn_id = ?
UNION ALL
SELECT relative_path FROM sequence_frame_scratch_leases WHERE run_id = ? AND turn_id = ?`,
		runID.String(), turnID.String(), runID.String(), turnID.String())
	if err != nil {
		return err
	}
	turnRoot := filepath.Join(repository.dataDir, "scratch", "runs", runID.String(), "turns", turnID.String())
	leaseRoots := map[string]struct{}{}
	for rows.Next() {
		var relative string
		if err := rows.Scan(&relative); err != nil {
			rows.Close()
			return err
		}
		path := filepath.Join(repository.dataDir, filepath.FromSlash(relative))
		inside, err := filepath.Rel(turnRoot, path)
		if err != nil || !validScratchRelative(inside) {
			rows.Close()
			return application.ErrAssetInvalid
		}
		parts := strings.Split(inside, string(filepath.Separator))
		if len(parts) < 2 || parts[0] == "agent" {
			rows.Close()
			return application.ErrAssetInvalid
		}
		leaseRoots[filepath.Join(turnRoot, parts[0])] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
DELETE FROM media_scratch_leases WHERE run_id = ? AND turn_id = ?`, runID.String(), turnID.String()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
DELETE FROM sequence_frame_scratch_leases WHERE run_id = ? AND turn_id = ?`, runID.String(), turnID.String()); err != nil {
		return err
	}
	for root := range leaseRoots {
		if err := os.RemoveAll(root); err != nil {
			return err
		}
	}
	_ = os.Remove(turnRoot)
	return nil
}

func (repository *SQLiteProjects) ReconcileMediaScratchLeases(ctx context.Context, now time.Time) error {
	return repository.ReconcileProductScratchLeases(ctx, now)
}

func (repository *SQLiteProjects) ReconcileProductScratchLeases(ctx context.Context, now time.Time) error {
	if now.IsZero() {
		return application.ErrAssetInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
SELECT 'media', lease.resource_id, lease.relative_path
FROM media_scratch_leases lease
JOIN agent_turns turn ON turn.id = lease.turn_id
JOIN agent_runs run ON run.id = lease.run_id
WHERE lease.expires_at <= ? OR turn.status <> 'active' OR run.current_turn_id <> lease.turn_id
UNION ALL
SELECT 'sequence-frame', lease.resource_id, lease.relative_path
FROM sequence_frame_scratch_leases lease
JOIN agent_turns turn ON turn.id = lease.turn_id
JOIN agent_runs run ON run.id = lease.run_id
WHERE lease.expires_at <= ? OR turn.status <> 'active' OR run.current_turn_id <> lease.turn_id`,
		formatInstant(now.UTC()), formatInstant(now.UTC()))
	if err != nil {
		return err
	}
	type staleLease struct {
		kind, id, relative string
	}
	stale := make([]staleLease, 0)
	for rows.Next() {
		var lease staleLease
		if err := rows.Scan(&lease.kind, &lease.id, &lease.relative); err != nil {
			rows.Close()
			return err
		}
		stale = append(stale, lease)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, lease := range stale {
		table := "media_scratch_leases"
		if lease.kind == "sequence-frame" {
			table = "sequence_frame_scratch_leases"
		} else if lease.kind != "media" {
			return application.ErrAssetInvalid
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE resource_id = ?`, lease.id); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, lease := range stale {
		if !validScratchRelative(filepath.FromSlash(lease.relative)) {
			return application.ErrAssetInvalid
		}
		path := filepath.Join(repository.dataDir, filepath.FromSlash(lease.relative))
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		_ = os.Remove(filepath.Dir(path))
	}
	return repository.reconcileScratchStorage(ctx)
}
