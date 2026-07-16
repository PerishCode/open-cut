package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) LoadSequenceExportPreparation(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	expectedSequenceRevision domain.Revision,
) (application.SequenceExportPreparationSnapshot, error) {
	if projectID.IsZero() || sequenceID.IsZero() || expectedSequenceRevision.Value() == 0 {
		return application.SequenceExportPreparationSnapshot{}, application.ErrSequenceExportInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.SequenceExportPreparationSnapshot{}, err
	}
	defer tx.Rollback()
	project, err := loadProjectProjection(ctx, tx, projectID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return application.SequenceExportPreparationSnapshot{}, application.ErrRenderSequenceNotFound
	}
	if err != nil {
		return application.SequenceExportPreparationSnapshot{}, err
	}
	if project.Status == domain.ProjectTombstoned || len(project.Sequences) != 1 ||
		project.Sequences[0].ID != sequenceID {
		return application.SequenceExportPreparationSnapshot{}, application.ErrRenderSequenceNotFound
	}
	sequence := project.Sequences[0]
	if sequence.Revision != expectedSequenceRevision {
		return application.SequenceExportPreparationSnapshot{}, application.ErrRenderSequenceConflict
	}
	clips, err := loadRenderClips(ctx, tx, projectID, sequenceID)
	if err != nil {
		return application.SequenceExportPreparationSnapshot{}, err
	}
	captions, err := loadRenderCaptions(ctx, tx, projectID, sequenceID)
	if err != nil {
		return application.SequenceExportPreparationSnapshot{}, err
	}
	assets, candidates, err := loadRenderAssetsAndCandidatesByKind(
		ctx, tx, projectID, clips, domain.ArtifactRenderInput,
	)
	if err != nil {
		return application.SequenceExportPreparationSnapshot{}, err
	}
	streams, err := loadSequencePreviewStreams(ctx, tx, clips, assets)
	if err != nil {
		return application.SequenceExportPreparationSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.SequenceExportPreparationSnapshot{}, err
	}
	loaded, err := repository.loadRenderCandidateManifests(candidates)
	if err != nil {
		return application.SequenceExportPreparationSnapshot{}, err
	}
	publicCandidates := make(map[string][]application.RenderMaterialCandidate, len(loaded))
	for assetID, records := range loaded {
		for _, candidate := range records {
			publicCandidates[assetID] = append(publicCandidates[assetID], application.RenderMaterialCandidate{
				ProducerJobID: candidate.producerJobID, Artifact: candidate.summary, Material: candidate.material,
			})
		}
	}
	return application.SequenceExportPreparationSnapshot{
		ProjectID: projectID, ObservedProjectRevision: project.Revision,
		Sequence: sequence, Clips: clips, Captions: captions, Assets: assets,
		Streams: streams, Candidates: publicCandidates,
	}, nil
}

func (repository *SQLiteProjects) LoadSequenceExportRetryPreparation(
	ctx context.Context,
	seed application.SequenceExportRetrySeed,
) (application.SequenceExportRetryPreparation, error) {
	parameters, intent := seed.Parameters, seed.RenderIntent
	if parameters.Validate() != nil || intent.Validate(parameters.Inputs) != nil ||
		parameters.ProjectID != intent.ProjectID || parameters.SequenceID != intent.SequenceID ||
		parameters.SequenceRevision != intent.SequenceRevision {
		return application.SequenceExportRetryPreparation{}, application.ErrSequenceExportInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.SequenceExportRetryPreparation{}, err
	}
	defer tx.Rollback()
	reusable := make(map[string]bool, len(parameters.Inputs))
	missingClips := make(map[string]struct{})
	for _, input := range parameters.Inputs {
		var available bool
		if err := tx.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM work_jobs job
  JOIN media_job_details detail ON detail.job_id = job.id
  WHERE job.id = ? AND job.kind = 'render-input' AND job.project_id = ?
    AND (
      job.state IN ('blocked', 'queued', 'running') OR
      (job.state = 'succeeded' AND EXISTS (
        SELECT 1 FROM media_artifacts artifact
        WHERE artifact.id = detail.result_artifact_id AND artifact.state = 'ready'
      ))
    )
)`, input.ProducerJobID.String(), parameters.ProjectID.String()).Scan(&available); err != nil {
			return application.SequenceExportRetryPreparation{}, err
		}
		reusable[input.ProducerJobID.String()] = available
		if !available {
			missingClips[input.ClipID.String()] = struct{}{}
		}
	}
	assets := make(map[string]application.RenderAssetSnapshot)
	clips := make([]domain.ClipState, 0, len(missingClips))
	intentAssets := make(map[string]application.SequencePreviewIntentAsset, len(intent.Assets))
	for _, asset := range intent.Assets {
		intentAssets[asset.ID.String()] = asset
	}
	for _, clip := range intent.Clips {
		if _, required := missingClips[clip.ID.String()]; !required {
			continue
		}
		asset, exists := intentAssets[clip.AssetID.String()]
		if !exists {
			return application.SequenceExportRetryPreparation{}, application.ErrSequenceExportInvalid
		}
		if _, loaded := assets[clip.AssetID.String()]; !loaded {
			var accepted, availability string
			if err := tx.QueryRowContext(ctx, `
SELECT asset.accepted_fingerprint, state.availability
FROM assets asset JOIN asset_media_state state ON state.asset_id = asset.id
WHERE asset.id = ? AND asset.project_id = ? AND asset.tombstoned = 0`,
				clip.AssetID.String(), parameters.ProjectID.String()).Scan(&accepted, &availability); errors.Is(err, sql.ErrNoRows) {
				return application.SequenceExportRetryPreparation{}, application.ErrRenderInputRequired
			} else if err != nil {
				return application.SequenceExportRetryPreparation{}, err
			}
			if accepted != asset.AcceptedFingerprint.String() ||
				(availability != string(domain.AssetOnline) && availability != string(domain.AssetManagedState)) {
				return application.SequenceExportRetryPreparation{}, application.ErrRenderInputRequired
			}
			assets[clip.AssetID.String()] = application.RenderAssetSnapshot{
				ID: clip.AssetID, Revision: asset.Revision,
				AcceptedFingerprint: asset.AcceptedFingerprint,
				Availability:        domain.AssetAvailability(availability),
			}
		}
		clips = append(clips, domain.ClipState{
			ID: clip.ID, Revision: clip.Revision, SequenceID: intent.SequenceID,
			TrackID: clip.TrackID, AssetID: clip.AssetID, SourceStreamID: clip.SourceStreamID,
			SourceRange: clip.SourceRange, TimelineRange: clip.TimelineRange, Enabled: true,
		})
	}
	streams, err := loadSequencePreviewStreams(ctx, tx, clips, assets)
	if err != nil {
		return application.SequenceExportRetryPreparation{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.SequenceExportRetryPreparation{}, err
	}
	return application.SequenceExportRetryPreparation{
		ProjectID: parameters.ProjectID, Assets: assets, Streams: streams,
		ReusableProducers: reusable,
	}, nil
}
