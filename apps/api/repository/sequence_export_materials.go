package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) ResolveSequenceExportArtifactRoots(
	ctx context.Context,
	claim application.WorkJobClaim,
	planDigest domain.Digest,
	inputs []domain.RenderPlanInput,
	observedAt time.Time,
) (map[string]string, error) {
	if claim.Kind != domain.WorkJobSequenceExport || claim.SequenceExport == nil ||
		claim.SequenceExport.ProjectID.IsZero() || !validStoredRenderDigest(planDigest) ||
		len(inputs) > application.MaximumRenderPlanItems || observedAt.IsZero() {
		return nil, application.ErrRenderPlanInvalid
	}
	projectID := claim.SequenceExport.ProjectID
	repository.artifactLifecycleMu.Lock()
	defer repository.artifactLifecycleMu.Unlock()
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := observedAt.UTC()
	if err := verifySequenceExportAttempt(ctx, tx, claim, now); err != nil {
		return nil, err
	}
	var storedProject string
	if err := tx.QueryRowContext(ctx, `
SELECT project_id FROM render_plans WHERE digest = ? AND purpose = 'export'`,
		planDigest.String(),
	).Scan(&storedProject); errors.Is(err, sql.ErrNoRows) {
		return nil, application.ErrRenderInputRequired
	} else if err != nil {
		return nil, err
	}
	if storedProject != projectID.String() {
		return nil, application.ErrRenderPlanInvalid
	}
	records := make([]storedRenderInputArtifact, len(inputs))
	for ordinal, input := range inputs {
		if input.ArtifactID.IsZero() || !validStoredRenderDigest(input.ArtifactDigest) ||
			input.Profile != application.RenderInputProfile {
			return nil, application.ErrRenderPlanInvalid
		}
		var idValue, assetValue, artifactDigest string
		err := tx.QueryRowContext(ctx, `
SELECT artifact.id, artifact.asset_id, artifact.producer_version,
       artifact.input_fingerprint, artifact.parameters_digest,
       artifact.parameters_json, artifact.byte_reference, artifact.byte_size,
       artifact.content_digest, pin.artifact_digest
FROM render_plan_inputs pin
JOIN media_artifacts artifact ON artifact.id = pin.artifact_id
WHERE pin.plan_digest = ? AND pin.ordinal = ?
  AND artifact.project_id = ? AND artifact.kind = 'render-input' AND artifact.state = 'ready'`,
			planDigest.String(), ordinal, projectID.String(),
		).Scan(&idValue, &assetValue, &records[ordinal].producer,
			&records[ordinal].inputFingerprint, &records[ordinal].parametersDigest,
			&records[ordinal].parametersJSON, &records[ordinal].byteReference,
			&records[ordinal].byteSize, &records[ordinal].contentDigest, &artifactDigest)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, application.ErrRenderInputRequired
		}
		if err != nil {
			return nil, err
		}
		records[ordinal].id, err = domain.ParseArtifactID(idValue)
		if err == nil {
			records[ordinal].assetID, err = domain.ParseAssetID(assetValue)
		}
		if err != nil || records[ordinal].id != input.ArtifactID ||
			records[ordinal].assetID != input.AssetID || records[ordinal].producer != input.ProducerVersion ||
			records[ordinal].inputFingerprint != input.Fingerprint.String() ||
			records[ordinal].contentDigest != input.ArtifactDigest.String() ||
			artifactDigest != input.ArtifactDigest.String() {
			return nil, application.ErrRenderPlanInvalid
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO render_material_leases (attempt_id, artifact_id, created_at)
VALUES (?, ?, ?) ON CONFLICT(attempt_id, artifact_id) DO NOTHING`,
			claim.AttemptID.String(), records[ordinal].id.String(), formatInstant(now),
		); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	result := make(map[string]string, len(records))
	for index, record := range records {
		if err := repository.verifyStoredRenderInputArtifact(record); err != nil {
			return nil, fmt.Errorf("verify render input %s: %w", record.id, err)
		}
		root := filepath.Join(repository.dataDir, "artifacts", "media", record.id.String())
		manifestBytes, err := readBoundedRegularFile(
			filepath.Join(root, "manifest.json"), application.MaximumRenderInputManifestSize,
		)
		if err != nil {
			return nil, err
		}
		manifest, err := application.DecodeRenderInputArtifactManifest(manifestBytes)
		if err != nil || !renderInputMatchesPlanInput(manifest, inputs[index]) {
			return nil, application.ErrRenderPlanInvalid
		}
		if err := verifyStoredRenderInputFileDigest(root, manifest.Media); err != nil {
			return nil, fmt.Errorf("verify render media %s: %w", record.id, err)
		}
		physical, err := filepath.EvalSymlinks(root)
		if err != nil || filepath.Clean(physical) == "" {
			return nil, application.ErrRenderInputRequired
		}
		info, err := os.Lstat(physical)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, application.ErrRenderInputRequired
		}
		result[record.id.String()] = filepath.Clean(physical)
	}
	return result, nil
}

func renderInputMatchesPlanInput(
	manifest application.RenderInputArtifactManifest,
	input domain.RenderPlanInput,
) bool {
	if _, _, err := application.CanonicalRenderInputArtifactManifest(manifest); err != nil ||
		manifest.AssetID != input.AssetID || manifest.Fingerprint != input.Fingerprint ||
		manifest.Profile != input.Profile || manifest.Producer != input.ProducerVersion ||
		manifest.SourceEpoch != input.SourceEpoch || manifest.Media.SHA256 != input.MediaDigest ||
		(manifest.Video == nil) != (input.Video == nil) || (manifest.Audio == nil) != (input.Audio == nil) {
		return false
	}
	if manifest.Video != nil {
		video := input.Video
		if manifest.Video.Source.ID != video.SourceStreamID ||
			manifest.Video.SourceStartTime != video.SourceStart ||
			manifest.Video.MaterialStartTime != video.MaterialStart ||
			manifest.Video.Source.Descriptor.TimeBase != video.SourceTimeBase ||
			manifest.Video.TimeBase != video.MaterialTimeBase ||
			manifest.Video.TimeMap.SHA256 != video.TimeMapDigest ||
			manifest.Video.Width != video.Width || manifest.Video.Height != video.Height {
			return false
		}
	}
	if manifest.Audio != nil {
		audio := input.Audio
		if manifest.Audio.Source.ID != audio.SourceStreamID ||
			manifest.Audio.SourceStartTime != audio.SourceStart ||
			manifest.Audio.MaterialStartTime != audio.MaterialStart ||
			manifest.Audio.Source.Descriptor.TimeBase != audio.SourceTimeBase ||
			manifest.Audio.TimeBase != audio.MaterialTimeBase ||
			manifest.Audio.SampleRate != audio.SampleRate ||
			manifest.Audio.ChannelLayout != audio.ChannelLayout ||
			manifest.Audio.DecodedSampleCount != audio.DecodedSampleCount {
			return false
		}
	}
	return true
}
