package repository

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) LoadBoundSequencePreviewRenderPlan(
	ctx context.Context,
	claim application.WorkJobClaim,
	now time.Time,
) (application.PublishedRenderPlan, bool, error) {
	if now.IsZero() {
		return application.PublishedRenderPlan{}, false, application.ErrSequencePreviewInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.PublishedRenderPlan{}, false, err
	}
	defer tx.Rollback()
	if err := verifySequencePreviewAttempt(ctx, tx, claim, now.UTC()); err != nil {
		return application.PublishedRenderPlan{}, false, err
	}
	var bound sql.NullString
	if err := tx.QueryRowContext(ctx, `
SELECT render_plan_digest FROM sequence_preview_job_details WHERE job_id = ?`,
		claim.JobID.String(),
	).Scan(&bound); err != nil {
		return application.PublishedRenderPlan{}, false, err
	}
	if !bound.Valid {
		if err := tx.Commit(); err != nil {
			return application.PublishedRenderPlan{}, false, err
		}
		return application.PublishedRenderPlan{}, false, nil
	}
	var canonicalJSON, createdAtValue string
	var observedRevisionValue uint64
	if err := tx.QueryRowContext(ctx, `
SELECT canonical_json, observed_project_revision, created_at
FROM render_plans WHERE digest = ?`, bound.String).Scan(
		&canonicalJSON, &observedRevisionValue, &createdAtValue,
	); err != nil {
		return application.PublishedRenderPlan{}, false, err
	}
	payload, digest, err := application.DecodeCanonicalRenderPlan([]byte(canonicalJSON))
	if err != nil || digest.String() != bound.String {
		return application.PublishedRenderPlan{}, false, application.ErrRenderPlanInvalid
	}
	observedRevision, err := domain.NewRevision(observedRevisionValue)
	if err != nil {
		return application.PublishedRenderPlan{}, false, application.ErrRenderPlanInvalid
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtValue)
	if err != nil {
		return application.PublishedRenderPlan{}, false, err
	}
	compiled := application.CompiledRenderPlan{
		Plan: domain.RenderPlan{
			Payload: payload, Digest: digest, ObservedProjectRevision: observedRevision,
		},
		Canonical: []byte(canonicalJSON),
	}
	binding := application.BindSequencePreviewRenderPlan{
		Claim: claim, Compiled: compiled, CreatedAt: now.UTC(),
	}
	if err := validateSequencePreviewPlanBinding(binding); err != nil {
		return application.PublishedRenderPlan{}, false, err
	}
	if err := verifySequencePreviewPlanPins(ctx, tx, binding); err != nil {
		return application.PublishedRenderPlan{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return application.PublishedRenderPlan{}, false, err
	}
	return application.PublishedRenderPlan{
		Plan: compiled.Plan, CreatedAt: createdAt.UTC(), Replayed: true,
	}, true, nil
}

func (repository *SQLiteProjects) LoadSequencePreviewRenderSnapshot(
	ctx context.Context,
	claim application.WorkJobClaim,
	now time.Time,
) (application.CompileRenderPlanInput, error) {
	if now.IsZero() {
		return application.CompileRenderPlanInput{}, application.ErrSequencePreviewInvalid
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.CompileRenderPlanInput{}, err
	}
	defer tx.Rollback()
	if err := verifySequencePreviewAttempt(ctx, tx, claim, now.UTC()); err != nil {
		return application.CompileRenderPlanInput{}, err
	}
	preview := claim.SequencePreview
	var intentSchema, intentDigestValue, intentJSON string
	if err := tx.QueryRowContext(ctx, `
SELECT render_intent_schema, render_intent_digest, render_intent_json
FROM sequence_preview_job_details WHERE job_id = ?`, claim.JobID.String()).Scan(
		&intentSchema, &intentDigestValue, &intentJSON,
	); err != nil {
		return application.CompileRenderPlanInput{}, err
	}
	intent, intentDigest, err := application.DecodeSequencePreviewRenderIntent(
		[]byte(intentJSON), preview.Parameters.Inputs,
	)
	if err != nil || intentSchema != application.SequencePreviewRenderIntentSchema ||
		intentDigest.String() != intentDigestValue || intent.ProjectID != preview.ProjectID ||
		intent.SequenceID != preview.SequenceID || intent.SequenceRevision != preview.SequenceRevision {
		return application.CompileRenderPlanInput{}, application.ErrSequencePreviewInvalid
	}
	compileSnapshot := intent.CompileInput(nil, nil)
	clipByID := make(map[string]domain.ClipState, len(compileSnapshot.Clips))
	for _, clip := range compileSnapshot.Clips {
		clipByID[clip.ID.String()] = clip
	}
	if len(clipByID) != len(preview.Parameters.Inputs) {
		return application.CompileRenderPlanInput{}, application.ErrSequencePreviewInvalid
	}
	candidates := make(map[string][]renderArtifactCandidate)
	producerAsset := make(map[string]string, len(preview.Parameters.Inputs))
	for _, input := range preview.Parameters.Inputs {
		clip, exists := clipByID[input.ClipID.String()]
		if !exists || clip.SourceStreamID != input.SourceStreamID {
			return application.CompileRenderPlanInput{}, application.ErrSequencePreviewInvalid
		}
		var projectValue, assetValue, artifactValue string
		err := tx.QueryRowContext(ctx, `
SELECT job.project_id, detail.asset_id, detail.result_artifact_id
FROM work_jobs job
JOIN media_job_details detail ON detail.job_id = job.id
JOIN media_artifacts artifact ON artifact.id = detail.result_artifact_id
WHERE job.id = ? AND job.kind = 'proxy' AND job.state = 'succeeded'
  AND artifact.state = 'ready' AND artifact.project_id = job.project_id
  AND artifact.asset_id = detail.asset_id`, input.ProducerJobID.String()).Scan(
			&projectValue, &assetValue, &artifactValue,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return application.CompileRenderPlanInput{}, application.ErrRenderInputRequired
		}
		if err != nil {
			return application.CompileRenderPlanInput{}, err
		}
		if projectValue != preview.ProjectID.String() || assetValue != clip.AssetID.String() {
			return application.CompileRenderPlanInput{}, application.ErrSequencePreviewInvalid
		}
		artifactID, err := domain.ParseArtifactID(artifactValue)
		if err != nil {
			return application.CompileRenderPlanInput{}, application.ErrSequencePreviewInvalid
		}
		summary, err := loadMediaArtifactSummary(ctx, tx, artifactID)
		if err != nil {
			return application.CompileRenderPlanInput{}, err
		}
		producerAsset[input.ProducerJobID.String()] = assetValue
		already := false
		for _, candidate := range candidates[assetValue] {
			if candidate.producerJobID == input.ProducerJobID {
				already = true
				break
			}
		}
		if !already {
			candidates[assetValue] = append(candidates[assetValue], renderArtifactCandidate{
				producerJobID: input.ProducerJobID, summary: summary,
			})
		}
	}
	if err := tx.Commit(); err != nil {
		return application.CompileRenderPlanInput{}, err
	}
	loaded, err := repository.loadRenderCandidateManifests(candidates)
	if err != nil {
		return application.CompileRenderPlanInput{}, err
	}
	byProducer := make(map[string]renderArtifactCandidate, len(preview.Parameters.Inputs))
	for assetValue, records := range loaded {
		for _, record := range records {
			if producerAsset[record.producerJobID.String()] != assetValue {
				return application.CompileRenderPlanInput{}, application.ErrSequencePreviewInvalid
			}
			byProducer[record.producerJobID.String()] = record
		}
	}
	bindings := make([]application.RenderClipInputBinding, 0, len(preview.Parameters.Inputs))
	for _, input := range preview.Parameters.Inputs {
		record, exists := byProducer[input.ProducerJobID.String()]
		if !exists || !renderManifestContainsStream(record.manifest, input.SourceStreamID) {
			return application.CompileRenderPlanInput{}, application.ErrRenderInputRequired
		}
		bindings = append(bindings, application.RenderClipInputBinding{
			ClipID: input.ClipID, Artifact: record.summary, Material: record.material,
		})
	}
	var font *domain.RenderFontResource
	for _, resource := range preview.Parameters.Resources {
		if resource.Kind != "font-bundle" || font != nil {
			return application.CompileRenderPlanInput{}, application.ErrSequencePreviewInvalid
		}
		font = &domain.RenderFontResource{
			ResourceID: resource.ID, Version: resource.Version, SHA256: resource.SHA256,
		}
	}
	return intent.CompileInput(bindings, font), nil
}

func (repository *SQLiteProjects) BindSequencePreviewRenderPlan(
	ctx context.Context,
	input application.BindSequencePreviewRenderPlan,
) (application.PublishedRenderPlan, error) {
	if err := validateSequencePreviewPlanBinding(input); err != nil {
		return application.PublishedRenderPlan{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.PublishedRenderPlan{}, err
	}
	defer tx.Rollback()
	if err := verifySequencePreviewAttempt(ctx, tx, input.Claim, input.CreatedAt.UTC()); err != nil {
		return application.PublishedRenderPlan{}, err
	}
	if err := verifySequencePreviewPlanPins(ctx, tx, input); err != nil {
		return application.PublishedRenderPlan{}, err
	}
	published, err := publishRenderPlanTx(ctx, tx, application.RenderPlanPublication{
		Compiled: input.Compiled, CreatedAt: input.CreatedAt.UTC(),
	})
	if err != nil {
		return application.PublishedRenderPlan{}, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE sequence_preview_job_details
SET render_plan_digest = ?
WHERE job_id = ? AND (render_plan_digest IS NULL OR render_plan_digest = ?)`,
		published.Plan.Digest.String(), input.Claim.JobID.String(), published.Plan.Digest.String(),
	)
	if err != nil {
		return application.PublishedRenderPlan{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return application.PublishedRenderPlan{}, application.ErrWorkLeaseLost
	}
	if err := tx.Commit(); err != nil {
		return application.PublishedRenderPlan{}, err
	}
	return published, nil
}

func verifySequencePreviewAttempt(
	ctx context.Context,
	tx *sql.Tx,
	claim application.WorkJobClaim,
	now time.Time,
) error {
	if claim.JobID.IsZero() || claim.AttemptID.IsZero() || claim.Kind != domain.WorkJobSequencePreview ||
		claim.SequencePreview == nil || claim.Media != nil || claim.LeaseOwner == "" || now.IsZero() {
		return application.ErrWorkLeaseLost
	}
	var (
		attemptState, owner, expires, attemptExecutor, jobState, jobKind string
		projectValue, digestValue, parametersJSON                        string
		sequenceValue, resolver, compiler, renderer, target, profile     string
		generation, sequenceRevision                                     uint64
	)
	err := tx.QueryRowContext(ctx, `
SELECT attempt.state, attempt.lease_owner, attempt.lease_expires_at,
       attempt.generation, attempt.executor_version,
       job.state, job.kind, job.project_id, job.parameters_digest, job.parameters_json,
       detail.sequence_id, detail.sequence_revision, detail.resolver_version,
       detail.compiler_version, detail.renderer_version, detail.renderer_target,
       detail.output_profile
FROM work_job_attempts attempt
JOIN work_jobs job ON job.id = attempt.job_id
JOIN sequence_preview_job_details detail ON detail.job_id = job.id
WHERE attempt.id = ? AND attempt.job_id = ?`, claim.AttemptID.String(), claim.JobID.String()).Scan(
		&attemptState, &owner, &expires, &generation, &attemptExecutor,
		&jobState, &jobKind, &projectValue, &digestValue, &parametersJSON,
		&sequenceValue, &sequenceRevision, &resolver, &compiler, &renderer, &target, &profile,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrWorkLeaseLost
	}
	if err != nil {
		return err
	}
	expiry, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return application.ErrWorkLeaseLost
	}
	preview := claim.SequencePreview
	parameters := preview.Parameters
	canonical, digest, normalized, canonicalErr := application.CanonicalSequencePreviewJobParameters(parameters)
	if canonicalErr != nil || normalized.ProjectID != preview.ProjectID ||
		normalized.SequenceID != preview.SequenceID || normalized.SequenceRevision != preview.SequenceRevision ||
		preview.ParametersDigest != digest || !bytes.Equal(preview.ParametersJSON, canonical) ||
		attemptState != "running" || owner != claim.LeaseOwner || generation != claim.Generation ||
		attemptExecutor != claim.ExecutorVersion || renderer != claim.ExecutorVersion ||
		jobState != "running" || jobKind != string(domain.WorkJobSequencePreview) ||
		projectValue != preview.ProjectID.String() || digestValue != digest.String() ||
		parametersJSON != string(canonical) || sequenceValue != preview.SequenceID.String() ||
		sequenceRevision != preview.SequenceRevision.Value() || resolver != parameters.ResolverVersion ||
		compiler != parameters.CompilerVersion || target != parameters.RendererTarget ||
		profile != parameters.OutputProfile || !expiry.After(now.UTC()) {
		return application.ErrWorkLeaseLost
	}
	return validateClaimedSequencePreviewPins(ctx, tx, claim.JobID, parameters)
}

func validateSequencePreviewPlanBinding(input application.BindSequencePreviewRenderPlan) error {
	claim, compiled := input.Claim, input.Compiled
	if input.CreatedAt.IsZero() || claim.SequencePreview == nil || claim.Media != nil ||
		claim.Kind != domain.WorkJobSequencePreview || compiled.Plan.Digest == "" ||
		len(compiled.Canonical) == 0 {
		return application.ErrSequencePreviewInvalid
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/render-plan", domain.RenderPlanSchema, compiled.Plan.Payload,
	)
	if err != nil || digest != compiled.Plan.Digest || !bytes.Equal(canonical, compiled.Canonical) {
		return application.ErrRenderPlanInvalid
	}
	preview, payload := claim.SequencePreview, compiled.Plan.Payload
	if payload.ProjectID != preview.ProjectID || payload.SequenceID != preview.SequenceID ||
		payload.SequenceRevision != preview.SequenceRevision ||
		payload.CompilerVersion != preview.Parameters.CompilerVersion ||
		payload.Output.Profile != preview.Parameters.OutputProfile ||
		compiled.Plan.ObservedProjectRevision.Value() == 0 {
		return application.ErrSequencePreviewInvalid
	}
	return nil
}

func verifySequencePreviewPlanPins(
	ctx context.Context,
	tx *sql.Tx,
	input application.BindSequencePreviewRenderPlan,
) error {
	payload := input.Compiled.Plan.Payload
	planInputs := make(map[string]domain.RenderPlanInput, len(payload.Inputs))
	for _, planInput := range payload.Inputs {
		planInputs[planInput.ArtifactID.String()] = planInput
	}
	video := make(map[string]domain.RenderVideoInstruction, len(payload.Video))
	for _, instruction := range payload.Video {
		video[instruction.ClipID.String()] = instruction
	}
	audio := make(map[string]domain.RenderAudioInstruction, len(payload.Audio))
	for _, instruction := range payload.Audio {
		audio[instruction.ClipID.String()] = instruction
	}
	for _, requirement := range input.Claim.SequencePreview.Parameters.Inputs {
		var assetValue, artifactValue, artifactDigest string
		err := tx.QueryRowContext(ctx, `
SELECT detail.asset_id, artifact.id, artifact.content_digest
FROM work_jobs producer
JOIN media_job_details detail ON detail.job_id = producer.id
JOIN media_artifacts artifact ON artifact.id = detail.result_artifact_id
WHERE producer.id = ? AND producer.kind = 'proxy' AND producer.state = 'succeeded'
  AND artifact.state = 'ready'`, requirement.ProducerJobID.String()).Scan(
			&assetValue, &artifactValue, &artifactDigest,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return application.ErrRenderInputRequired
		}
		if err != nil {
			return err
		}
		planInput, exists := planInputs[artifactValue]
		if !exists || planInput.AssetID.String() != assetValue ||
			planInput.ArtifactDigest.String() != artifactDigest {
			return application.ErrSequencePreviewInvalid
		}
		if instruction, exists := video[requirement.ClipID.String()]; exists {
			if instruction.InputArtifactID.String() != artifactValue ||
				instruction.SourceStreamID != requirement.SourceStreamID {
				return application.ErrSequencePreviewInvalid
			}
			continue
		}
		if instruction, exists := audio[requirement.ClipID.String()]; !exists ||
			instruction.InputArtifactID.String() != artifactValue ||
			instruction.SourceStreamID != requirement.SourceStreamID {
			return application.ErrSequencePreviewInvalid
		}
	}
	if len(payload.FontResources) != len(input.Claim.SequencePreview.Parameters.Resources) {
		return application.ErrSequencePreviewInvalid
	}
	for index, resource := range input.Claim.SequencePreview.Parameters.Resources {
		font := payload.FontResources[index]
		if resource.Kind != "font-bundle" || font.ResourceID != resource.ID ||
			font.Version != resource.Version || font.SHA256 != resource.SHA256 {
			return application.ErrSequencePreviewInvalid
		}
	}
	return nil
}
