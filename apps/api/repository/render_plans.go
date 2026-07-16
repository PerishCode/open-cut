package repository

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sort"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type renderArtifactCandidate struct {
	producerJobID domain.WorkJobID
	summary       domain.ArtifactSummary
	manifest      application.SourceProxyArtifactManifest
	material      application.RenderMaterial
}

func (repository *SQLiteProjects) LoadSequenceRenderSnapshot(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	expectedSequenceRevision domain.Revision,
) (application.CompileRenderPlanInput, error) {
	if projectID.IsZero() || sequenceID.IsZero() || expectedSequenceRevision.Value() == 0 {
		return application.CompileRenderPlanInput{}, application.ErrRenderPlanInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.CompileRenderPlanInput{}, err
	}
	defer tx.Rollback()
	project, err := loadProjectProjection(ctx, tx, projectID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return application.CompileRenderPlanInput{}, application.ErrRenderSequenceNotFound
	}
	if err != nil {
		return application.CompileRenderPlanInput{}, err
	}
	if project.Status == domain.ProjectTombstoned || len(project.Sequences) != 1 ||
		project.Sequences[0].ID != sequenceID {
		return application.CompileRenderPlanInput{}, application.ErrRenderSequenceNotFound
	}
	sequence := project.Sequences[0]
	if sequence.Revision != expectedSequenceRevision {
		return application.CompileRenderPlanInput{}, application.ErrRenderSequenceConflict
	}
	clips, err := loadRenderClips(ctx, tx, projectID, sequenceID)
	if err != nil {
		return application.CompileRenderPlanInput{}, err
	}
	captions, err := loadRenderCaptions(ctx, tx, projectID, sequenceID)
	if err != nil {
		return application.CompileRenderPlanInput{}, err
	}
	assets, candidates, err := loadRenderAssetsAndCandidates(ctx, tx, projectID, clips)
	if err != nil {
		return application.CompileRenderPlanInput{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.CompileRenderPlanInput{}, err
	}
	loaded, err := repository.loadRenderCandidateManifests(candidates)
	if err != nil {
		return application.CompileRenderPlanInput{}, err
	}
	bindings, err := selectRenderBindings(clips, loaded)
	if err != nil {
		return application.CompileRenderPlanInput{}, err
	}
	return application.CompileRenderPlanInput{
		ProjectID: projectID, ObservedProjectRevision: project.Revision,
		Sequence: sequence, Clips: clips, Captions: captions, Assets: assets, Bindings: bindings,
	}, nil
}

func loadRenderClips(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
) ([]domain.ClipState, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id FROM clips
WHERE project_id = ? AND sequence_id = ? AND tombstoned = 0
ORDER BY track_id, timeline_start_order_key, id LIMIT ?`,
		projectID.String(), sequenceID.String(), application.MaximumRenderPlanItems+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]domain.ClipID, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		id, err := domain.ParseClipID(value)
		if err != nil {
			return nil, application.ErrRenderPlanInvalid
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > application.MaximumRenderPlanItems {
		return nil, application.ErrRenderPlanInvalid
	}
	clips := make([]domain.ClipState, 0, len(ids))
	for _, id := range ids {
		clip, err := loadClipState(ctx, tx, projectID, id)
		if err != nil {
			return nil, err
		}
		clips = append(clips, clip)
	}
	return clips, nil
}

func loadRenderCaptions(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
) ([]domain.CaptionState, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id FROM captions
WHERE project_id = ? AND sequence_id = ? AND tombstoned = 0
ORDER BY track_id, start_order_key, id LIMIT ?`,
		projectID.String(), sequenceID.String(), application.MaximumRenderPlanItems+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]domain.CaptionID, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		id, err := domain.ParseCaptionID(value)
		if err != nil {
			return nil, application.ErrRenderPlanInvalid
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > application.MaximumRenderPlanItems {
		return nil, application.ErrRenderPlanInvalid
	}
	captions := make([]domain.CaptionState, 0, len(ids))
	for _, id := range ids {
		caption, err := loadCaptionState(ctx, tx, projectID, id)
		if err != nil {
			return nil, err
		}
		captions = append(captions, caption)
	}
	return captions, nil
}

func loadRenderAssetsAndCandidates(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	clips []domain.ClipState,
) (map[string]application.RenderAssetSnapshot, map[string][]renderArtifactCandidate, error) {
	return loadRenderAssetsAndCandidatesByKind(ctx, tx, projectID, clips, domain.ArtifactProxy)
}

func loadRenderAssetsAndCandidatesByKind(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	clips []domain.ClipState,
	kind domain.ArtifactKind,
) (map[string]application.RenderAssetSnapshot, map[string][]renderArtifactCandidate, error) {
	if kind != domain.ArtifactProxy && kind != domain.ArtifactRenderInput {
		return nil, nil, application.ErrRenderInputRequired
	}
	assets := make(map[string]application.RenderAssetSnapshot)
	candidates := make(map[string][]renderArtifactCandidate)
	for _, clip := range clips {
		if !clip.Enabled {
			continue
		}
		key := clip.AssetID.String()
		if _, exists := assets[key]; exists {
			continue
		}
		var idValue, fingerprintValue, availability string
		var revisionValue uint64
		err := tx.QueryRowContext(ctx, `
SELECT asset.id, asset.revision, asset.accepted_fingerprint, media.availability
FROM assets asset
JOIN asset_media_state media ON media.asset_id = asset.id
WHERE asset.id = ? AND asset.project_id = ? AND asset.tombstoned = 0
  AND asset.accepted_fingerprint IS NOT NULL`,
			key, projectID.String()).Scan(&idValue, &revisionValue, &fingerprintValue, &availability)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, application.ErrRenderInputRequired
		}
		if err != nil {
			return nil, nil, err
		}
		id, parseErr := domain.ParseAssetID(idValue)
		revision, revisionErr := domain.NewRevision(revisionValue)
		fingerprint, digestErr := domain.ParseDigest(fingerprintValue)
		if parseErr != nil || revisionErr != nil || digestErr != nil {
			return nil, nil, application.ErrRenderPlanInvalid
		}
		assets[key] = application.RenderAssetSnapshot{
			ID: id, Revision: revision, AcceptedFingerprint: fingerprint,
			Availability: domain.AssetAvailability(availability),
		}
		rows, queryErr := tx.QueryContext(ctx, `
SELECT artifact.id, detail.job_id
FROM media_artifacts artifact
JOIN media_job_details detail ON detail.result_artifact_id = artifact.id
JOIN work_jobs job ON job.id = detail.job_id AND job.state = 'succeeded'
WHERE artifact.project_id = ? AND artifact.asset_id = ?
  AND artifact.kind = ? AND artifact.state = 'ready'
  AND artifact.input_fingerprint = ?
ORDER BY artifact.parameters_digest, artifact.id, job.created_at DESC, job.id DESC
LIMIT 1025`, projectID.String(), key, string(kind), fingerprintValue)
		if queryErr != nil {
			return nil, nil, queryErr
		}
		producers := make(map[string]domain.WorkJobID)
		artifactIDs := make([]domain.ArtifactID, 0)
		for rows.Next() {
			var artifactValue, producerValue string
			if scanErr := rows.Scan(&artifactValue, &producerValue); scanErr != nil {
				rows.Close()
				return nil, nil, scanErr
			}
			artifactID, parseErr := domain.ParseArtifactID(artifactValue)
			producerID, producerErr := domain.ParseWorkJobID(producerValue)
			if parseErr != nil || producerErr != nil {
				rows.Close()
				return nil, nil, application.ErrRenderPlanInvalid
			}
			if _, exists := producers[artifactValue]; !exists {
				producers[artifactValue] = producerID
				artifactIDs = append(artifactIDs, artifactID)
			}
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			rows.Close()
			return nil, nil, rowsErr
		}
		if closeErr := rows.Close(); closeErr != nil {
			return nil, nil, closeErr
		}
		for _, artifactID := range artifactIDs {
			summary, loadErr := loadMediaArtifactSummary(ctx, tx, artifactID)
			if loadErr != nil {
				return nil, nil, loadErr
			}
			candidates[key] = append(candidates[key], renderArtifactCandidate{
				producerJobID: producers[artifactID.String()], summary: summary,
			})
		}
		if len(candidates[key]) > 256 {
			return nil, nil, application.ErrRenderInputRequired
		}
	}
	return assets, candidates, nil
}

func loadRenderAssets(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	clips []domain.ClipState,
) (map[string]application.RenderAssetSnapshot, error) {
	assets := make(map[string]application.RenderAssetSnapshot)
	for _, clip := range clips {
		if !clip.Enabled || clip.Tombstoned {
			continue
		}
		key := clip.AssetID.String()
		if _, exists := assets[key]; exists {
			continue
		}
		var idValue, fingerprintValue, availability string
		var revisionValue uint64
		err := tx.QueryRowContext(ctx, `
SELECT asset.id, asset.revision, asset.accepted_fingerprint, media.availability
FROM assets asset
JOIN asset_media_state media ON media.asset_id = asset.id
WHERE asset.id = ? AND asset.project_id = ? AND asset.tombstoned = 0
  AND asset.accepted_fingerprint IS NOT NULL`, key, projectID.String()).Scan(
			&idValue, &revisionValue, &fingerprintValue, &availability,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, application.ErrRenderInputRequired
		}
		if err != nil {
			return nil, err
		}
		id, parseErr := domain.ParseAssetID(idValue)
		revision, revisionErr := domain.NewRevision(revisionValue)
		fingerprint, digestErr := domain.ParseDigest(fingerprintValue)
		if parseErr != nil || revisionErr != nil || digestErr != nil {
			return nil, application.ErrRenderPlanInvalid
		}
		assets[key] = application.RenderAssetSnapshot{
			ID: id, Revision: revision, AcceptedFingerprint: fingerprint,
			Availability: domain.AssetAvailability(availability),
		}
	}
	return assets, nil
}

func (repository *SQLiteProjects) loadRenderCandidateManifests(
	candidates map[string][]renderArtifactCandidate,
) (map[string][]renderArtifactCandidate, error) {
	result := make(map[string][]renderArtifactCandidate, len(candidates))
	for assetID, candidates := range candidates {
		for _, candidate := range candidates {
			path := filepath.Join(repository.dataDir, "artifacts", "media", candidate.summary.ID.String(), "manifest.json")
			data, err := readBoundedRegularFile(path, application.MaximumRenderInputManifestSize)
			if err != nil {
				continue
			}
			var material application.RenderMaterial
			switch candidate.summary.Kind {
			case domain.ArtifactProxy:
				manifest, decodeErr := application.DecodeSourceProxyArtifactManifest(data)
				if decodeErr != nil {
					continue
				}
				candidate.manifest = manifest
				material, err = application.NewSourceProxyRenderMaterial(manifest)
			case domain.ArtifactRenderInput:
				manifest, decodeErr := application.DecodeRenderInputArtifactManifest(data)
				if decodeErr != nil {
					continue
				}
				material, err = application.NewRenderInputRenderMaterial(manifest)
			default:
				continue
			}
			if err != nil {
				continue
			}
			candidate.material = material
			result[assetID] = append(result[assetID], candidate)
		}
		sort.Slice(result[assetID], func(left, right int) bool {
			return result[assetID][left].summary.ID.String() < result[assetID][right].summary.ID.String()
		})
	}
	return result, nil
}

func selectRenderBindings(
	clips []domain.ClipState,
	candidates map[string][]renderArtifactCandidate,
) ([]application.RenderClipInputBinding, error) {
	result := make([]application.RenderClipInputBinding, 0, len(clips))
	for _, clip := range clips {
		if !clip.Enabled {
			continue
		}
		var selected *renderArtifactCandidate
		for index := range candidates[clip.AssetID.String()] {
			candidate := &candidates[clip.AssetID.String()][index]
			if candidate.material.ContainsStream(clip.SourceStreamID) {
				selected = candidate
				break
			}
		}
		if selected == nil {
			return nil, application.ErrRenderInputRequired
		}
		result = append(result, application.RenderClipInputBinding{
			ClipID: clip.ID, Artifact: selected.summary, Material: selected.material,
		})
	}
	return result, nil
}

func renderManifestContainsStream(
	manifest application.SourceProxyArtifactManifest,
	streamID domain.SourceStreamID,
) bool {
	return manifest.Video != nil && manifest.Video.Source.ID == streamID ||
		manifest.Audio != nil && manifest.Audio.Source.ID == streamID
}

func (repository *SQLiteProjects) PublishRenderPlan(
	ctx context.Context,
	publication application.RenderPlanPublication,
) (application.PublishedRenderPlan, error) {
	if err := validateRenderPlanPublication(publication); err != nil {
		return application.PublishedRenderPlan{}, application.ErrRenderPlanInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.PublishedRenderPlan{}, err
	}
	defer tx.Rollback()
	published, err := publishRenderPlanTx(ctx, tx, publication)
	if err != nil {
		return application.PublishedRenderPlan{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.PublishedRenderPlan{}, err
	}
	return published, nil
}

func validateRenderPlanPublication(publication application.RenderPlanPublication) error {
	compiled, at := publication.Compiled, publication.CreatedAt.UTC()
	if at.IsZero() || compiled.Plan.Digest == "" || len(compiled.Canonical) == 0 ||
		compiled.Plan.ObservedProjectRevision.Value() == 0 {
		return application.ErrRenderPlanInvalid
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/render-plan", domain.RenderPlanSchema, compiled.Plan.Payload,
	)
	if err != nil || digest != compiled.Plan.Digest || !bytes.Equal(canonical, compiled.Canonical) {
		return application.ErrRenderPlanInvalid
	}
	return nil
}

func publishRenderPlanTx(
	ctx context.Context,
	tx *sql.Tx,
	publication application.RenderPlanPublication,
) (application.PublishedRenderPlan, error) {
	if err := validateRenderPlanPublication(publication); err != nil {
		return application.PublishedRenderPlan{}, err
	}
	compiled, at := publication.Compiled, publication.CreatedAt.UTC()
	canonical, digest, _ := domain.CanonicalDigest(
		"open-cut/render-plan", domain.RenderPlanSchema, compiled.Plan.Payload,
	)
	var storedCanonical, storedCreated string
	var storedObservedRevision uint64
	err := tx.QueryRowContext(ctx, `
SELECT canonical_json, observed_project_revision, created_at
FROM render_plans WHERE digest = ?`, digest.String()).Scan(
		&storedCanonical, &storedObservedRevision, &storedCreated,
	)
	if err == nil {
		if storedCanonical != string(canonical) {
			return application.PublishedRenderPlan{}, application.ErrRenderPlanInvalid
		}
		created, parseErr := time.Parse(time.RFC3339Nano, storedCreated)
		if parseErr != nil {
			return application.PublishedRenderPlan{}, parseErr
		}
		observedRevision, parseErr := domain.NewRevision(storedObservedRevision)
		if parseErr != nil {
			return application.PublishedRenderPlan{}, parseErr
		}
		storedPlan := compiled.Plan
		storedPlan.ObservedProjectRevision = observedRevision
		return application.PublishedRenderPlan{Plan: storedPlan, CreatedAt: created, Replayed: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return application.PublishedRenderPlan{}, err
	}
	for _, input := range compiled.Plan.Payload.Inputs {
		var state, storedDigest string
		if err := tx.QueryRowContext(ctx, `
SELECT state, content_digest FROM media_artifacts
WHERE id = ? AND project_id = ? AND asset_id = ? AND input_fingerprint = ?`,
			input.ArtifactID.String(), compiled.Plan.Payload.ProjectID.String(), input.AssetID.String(),
			input.Fingerprint.String()).Scan(&state, &storedDigest); err != nil ||
			state != string(domain.ArtifactReady) || storedDigest != input.ArtifactDigest.String() {
			return application.PublishedRenderPlan{}, application.ErrRenderInputRequired
		}
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO render_plans (
  digest, schema_version, compiler_version, purpose, project_id, sequence_id,
  sequence_revision, observed_project_revision, output_profile, canonical_json, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		digest.String(), domain.RenderPlanSchema, compiled.Plan.Payload.CompilerVersion,
		string(compiled.Plan.Payload.Purpose), compiled.Plan.Payload.ProjectID.String(),
		compiled.Plan.Payload.SequenceID.String(), compiled.Plan.Payload.SequenceRevision.Value(),
		compiled.Plan.ObservedProjectRevision.Value(), compiled.Plan.Payload.Output.Profile,
		string(canonical), at.Format(time.RFC3339Nano)); err != nil {
		return application.PublishedRenderPlan{}, err
	}
	for index, input := range compiled.Plan.Payload.Inputs {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO render_plan_inputs (plan_digest, ordinal, artifact_id, artifact_digest)
VALUES (?, ?, ?, ?)`, digest.String(), index, input.ArtifactID.String(), input.ArtifactDigest.String()); err != nil {
			return application.PublishedRenderPlan{}, err
		}
	}
	return application.PublishedRenderPlan{Plan: compiled.Plan, CreatedAt: at, Replayed: false}, nil
}
