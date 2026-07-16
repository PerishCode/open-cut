package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) ReplaySequenceExportRequest(
	ctx context.Context,
	record application.ReplaySequenceExportRequestRecord,
) (application.SequenceExportResult, bool, error) {
	if err := validateSequenceExportReplayRecord(record); err != nil {
		return application.SequenceExportResult{}, false, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.SequenceExportResult{}, false, err
	}
	defer tx.Rollback()
	if err := verifySequenceExportAccess(ctx, tx, record.ReadSequenceExportRecord, false); err != nil {
		return application.SequenceExportResult{}, false, err
	}
	result, found, err := replaySequenceExportRequestTx(ctx, tx, record)
	if err != nil || !found {
		return result, found, err
	}
	if err := tx.Commit(); err != nil {
		return application.SequenceExportResult{}, false, err
	}
	result.Replayed = true
	return result, true, nil
}

func (repository *SQLiteProjects) RequestSequenceExport(
	ctx context.Context,
	record application.RequestSequenceExportRecord,
) (application.SequenceExportResult, error) {
	if err := application.ValidateSequenceExportRequestRecord(record); err != nil {
		return application.SequenceExportResult{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	defer tx.Rollback()
	read := application.ReadSequenceExportRecord{
		ProjectID: record.ProjectID, RunID: record.RunID, TurnID: record.TurnID,
		Actor: record.Actor, Owner: record.Owner, JobID: record.JobID,
	}
	if err := verifySequenceExportAccess(ctx, tx, read, false); err != nil {
		return application.SequenceExportResult{}, err
	}
	replayRecord := application.ReplaySequenceExportRequestRecord{
		ReadSequenceExportRecord: read, Command: "start", RequestID: record.RequestID,
		RequestDigest: record.RequestDigest, RequestCanonical: record.RequestCanonical,
	}
	if replay, found, err := replaySequenceExportRequestTx(ctx, tx, replayRecord); err != nil {
		return application.SequenceExportResult{}, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return application.SequenceExportResult{}, err
		}
		replay.Replayed = true
		return replay, nil
	}
	if err := validateSequenceExportPins(ctx, tx, record); err != nil {
		return application.SequenceExportResult{}, err
	}
	at := formatInstant(record.RequestedAt.UTC())
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_jobs (
  id, scope_kind, project_id, kind, state, pool, priority_class, logical_key,
  parameters_digest, parameters_json, producer_version, created_at, updated_at
) VALUES (?, 'project', ?, 'sequence-export', 'blocked', 'cpu', 'foreground', ?, ?, ?, ?, ?, ?)`,
		record.JobID.String(), record.ProjectID.String(), record.LogicalKey,
		record.ParametersDigest.String(), string(record.ParametersJSON),
		record.Parameters.RendererVersion, at, at,
	); err != nil {
		return application.SequenceExportResult{}, err
	}
	if err := insertSequenceExportDetails(ctx, tx, record); err != nil {
		return application.SequenceExportResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
VALUES (?, 'executor-required', 'capability', 'work-executor/sequence-export', ?)`,
		record.JobID.String(), at,
	); err != nil {
		return application.SequenceExportResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_owners (job_id, owner_kind, owner_id, created_at)
VALUES (?, ?, ?, ?)`, record.JobID.String(), string(record.Owner.Kind), record.Owner.ID, at); err != nil {
		return application.SequenceExportResult{}, err
	}
	if err := appendSequenceExportActivity(
		ctx, tx, record.ProjectID, record.RunID, record.TurnID, record.Actor,
		record.JobID, record.ActivityEventID, "sequence.export-requested",
		"sequence-export-requested", record.RequestedAt,
	); err != nil {
		return application.SequenceExportResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_export_requests (
  actor_id, request_id, command, input_digest, input_json, project_id,
  owner_kind, owner_id, run_id, turn_id, job_id, activity_event_id, created_at
) VALUES (?, ?, 'start', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Actor.IDString(), record.RequestID.String(), record.RequestDigest.String(),
		string(record.RequestCanonical), record.ProjectID.String(), string(record.Owner.Kind), record.Owner.ID,
		sequenceExportOptionalID(record.RunID.String()), sequenceExportOptionalID(record.TurnID.String()),
		record.JobID.String(), record.ActivityEventID.String(), at,
	); err != nil {
		return application.SequenceExportResult{}, err
	}
	result, err := loadSequenceExportResult(ctx, tx, record.ProjectID, record.JobID)
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.SequenceExportResult{}, err
	}
	return result, nil
}

func (repository *SQLiteProjects) ReadSequenceExport(
	ctx context.Context,
	record application.ReadSequenceExportRecord,
) (application.SequenceExportResult, error) {
	if err := validateSequenceExportReadRecord(record); err != nil {
		return application.SequenceExportResult{}, err
	}
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	defer tx.Rollback()
	if err := verifySequenceExportAccess(ctx, tx, record, true); err != nil {
		return application.SequenceExportResult{}, err
	}
	tail, err := resolveSequenceExportTailID(ctx, tx, record.ProjectID, record.JobID)
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	result, err := loadSequenceExportResult(ctx, tx, record.ProjectID, tail)
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.SequenceExportResult{}, err
	}
	return result, nil
}

func replaySequenceExportRequestTx(
	ctx context.Context,
	tx *sql.Tx,
	record application.ReplaySequenceExportRequestRecord,
) (application.SequenceExportResult, bool, error) {
	var command, digest, canonical, projectValue, ownerKind, ownerID, jobValue string
	var runValue, turnValue sql.NullString
	err := tx.QueryRowContext(ctx, `
SELECT command, input_digest, input_json, project_id, owner_kind, owner_id, run_id, turn_id, job_id
FROM sequence_export_requests WHERE actor_id = ? AND request_id = ?`,
		record.Actor.IDString(), record.RequestID.String(),
	).Scan(&command, &digest, &canonical, &projectValue, &ownerKind, &ownerID, &runValue, &turnValue, &jobValue)
	if errors.Is(err, sql.ErrNoRows) {
		return application.SequenceExportResult{}, false, nil
	}
	if err != nil {
		return application.SequenceExportResult{}, false, err
	}
	if command != record.Command || digest != record.RequestDigest.String() ||
		canonical != string(record.RequestCanonical) || projectValue != record.ProjectID.String() ||
		ownerKind != string(record.Owner.Kind) || ownerID != record.Owner.ID ||
		runValue.String != record.RunID.String() || runValue.Valid != !record.RunID.IsZero() ||
		turnValue.String != record.TurnID.String() || turnValue.Valid != !record.TurnID.IsZero() {
		return application.SequenceExportResult{}, false, application.ErrSequenceExportReused
	}
	jobID, err := domain.ParseWorkJobID(jobValue)
	if err != nil {
		return application.SequenceExportResult{}, false, application.ErrSequenceExportInvalid
	}
	tail, err := resolveSequenceExportTailID(ctx, tx, record.ProjectID, jobID)
	if err != nil {
		return application.SequenceExportResult{}, false, err
	}
	result, err := loadSequenceExportResult(ctx, tx, record.ProjectID, tail)
	return result, err == nil, err
}

func insertSequenceExportDetails(
	ctx context.Context,
	tx *sql.Tx,
	record application.RequestSequenceExportRecord,
) error {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_export_job_details (
  job_id, sequence_id, sequence_revision, preset, resolver_version, compiler_version,
  renderer_version, renderer_target, render_intent_schema, render_intent_digest, render_intent_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.JobID.String(), record.SequenceID.String(), record.Parameters.SequenceRevision.Value(),
		record.Parameters.Preset, record.Parameters.ResolverVersion, record.Parameters.CompilerVersion,
		record.Parameters.RendererVersion, record.Parameters.RendererTarget,
		application.SequenceRenderIntentSchema, record.IntentDigest.String(), string(record.IntentJSON),
	); err != nil {
		return err
	}
	at := formatInstant(record.RequestedAt.UTC())
	producers := make(map[string]struct{})
	for ordinal, input := range record.Parameters.Inputs {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_export_job_inputs (job_id, ordinal, clip_id, source_stream_id, producer_job_id)
VALUES (?, ?, ?, ?, ?)`, record.JobID.String(), ordinal, input.ClipID.String(),
			input.SourceStreamID.String(), input.ProducerJobID.String()); err != nil {
			return err
		}
		if _, exists := producers[input.ProducerJobID.String()]; !exists {
			producers[input.ProducerJobID.String()] = struct{}{}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO work_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
VALUES (?, 'artifact-ready', 'job', ?, ?)`, record.JobID.String(), input.ProducerJobID.String(), at); err != nil {
				return err
			}
		}
	}
	for ordinal, resource := range record.Parameters.Resources {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO sequence_export_job_resources (
  job_id, ordinal, resource_kind, resource_id, resource_version, resource_digest
) VALUES (?, ?, ?, ?, ?, ?)`, record.JobID.String(), ordinal, resource.Kind,
			resource.ID, resource.Version, resource.SHA256.String()); err != nil {
			return err
		}
	}
	return nil
}

func validateSequenceExportPins(
	ctx context.Context,
	tx *sql.Tx,
	record application.RequestSequenceExportRecord,
) error {
	var revision uint64
	if err := tx.QueryRowContext(ctx, `
SELECT sequence.revision FROM sequences sequence
JOIN projects project ON project.id = sequence.project_id
WHERE sequence.id = ? AND sequence.project_id = ? AND project.status = 'active'`,
		record.SequenceID.String(), record.ProjectID.String(),
	).Scan(&revision); errors.Is(err, sql.ErrNoRows) {
		return application.ErrRenderSequenceNotFound
	} else if err != nil {
		return err
	}
	if revision != record.Parameters.SequenceRevision.Value() {
		return application.ErrRenderSequenceConflict
	}
	for _, input := range record.Parameters.Inputs {
		var clipStream, clipAsset, producerAsset, kind, producerProject string
		if err := tx.QueryRowContext(ctx, `
SELECT clip.source_stream_id, clip.asset_id, detail.asset_id, job.kind, job.project_id
FROM clips clip
JOIN media_job_details detail ON detail.job_id = ?
JOIN work_jobs job ON job.id = detail.job_id
WHERE clip.id = ? AND clip.project_id = ? AND clip.sequence_id = ?
  AND clip.tombstoned = 0 AND clip.enabled = 1`,
			input.ProducerJobID.String(), input.ClipID.String(), record.ProjectID.String(), record.SequenceID.String(),
		).Scan(&clipStream, &clipAsset, &producerAsset, &kind, &producerProject); err != nil {
			return application.ErrRenderInputRequired
		}
		if clipStream != input.SourceStreamID.String() || clipAsset != producerAsset ||
			kind != string(domain.MediaJobRenderInput) || producerProject != record.ProjectID.String() {
			return application.ErrRenderInputRequired
		}
	}
	for _, asset := range record.RenderIntent.Assets {
		var revision uint64
		var fingerprint string
		if err := tx.QueryRowContext(ctx, `
SELECT revision, accepted_fingerprint FROM assets
WHERE id = ? AND project_id = ? AND tombstoned = 0`,
			asset.ID.String(), record.ProjectID.String(),
		).Scan(&revision, &fingerprint); err != nil || revision != asset.Revision.Value() ||
			fingerprint != asset.AcceptedFingerprint.String() {
			return application.ErrRenderInputRequired
		}
	}
	return nil
}

func verifySequenceExportAccess(
	ctx context.Context,
	tx *sql.Tx,
	record application.ReadSequenceExportRecord,
	requireOwnership bool,
) error {
	if record.Owner.Validate(record.Actor, record.RunID, record.TurnID) != nil {
		return fmt.Errorf("%w: access owner", application.ErrSequenceExportInvalid)
	}
	if record.Owner.Kind == application.SequenceExportOwnerCreator {
		query := `SELECT 1 FROM projects WHERE id = ? AND status = 'active'`
		args := []any{record.ProjectID.String()}
		if requireOwnership {
			query += ` AND EXISTS (
  SELECT 1 FROM work_jobs job
  JOIN sequence_export_job_details detail ON detail.job_id = job.id
  WHERE job.id = ? AND job.project_id = projects.id AND job.kind = 'sequence-export'
)`
			args = append(args, record.JobID.String())
		}
		var valid int
		if err := tx.QueryRowContext(ctx, query, args...).Scan(&valid); errors.Is(err, sql.ErrNoRows) {
			return application.ErrSequenceExportNotFound
		} else {
			return err
		}
	}
	var valid int
	query := `
SELECT 1 FROM agent_runs run
JOIN agent_turns turn ON turn.id = run.current_turn_id AND turn.run_id = run.id
WHERE run.id = ? AND run.project_id = ? AND run.actor_id = ?
  AND run.status IN ('active', 'waiting') AND turn.id = ? AND turn.status = 'active'`
	args := []any{record.RunID.String(), record.ProjectID.String(), record.Actor.IDString(), record.TurnID.String()}
	if requireOwnership {
		query += ` AND EXISTS (
  SELECT 1 FROM work_job_owners owner
  JOIN work_jobs job ON job.id = owner.job_id
  JOIN sequence_export_job_details detail ON detail.job_id = job.id
  WHERE owner.job_id = ? AND owner.owner_kind = 'run' AND owner.owner_id = run.id
    AND job.project_id = run.project_id
)`
		args = append(args, record.JobID.String())
	}
	err := tx.QueryRowContext(ctx, query, args...).Scan(&valid)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrRunStaleTurn
	}
	return err
}

func validateSequenceExportReadRecord(record application.ReadSequenceExportRecord) error {
	if record.ProjectID.IsZero() || record.JobID.IsZero() ||
		record.Owner.Validate(record.Actor, record.RunID, record.TurnID) != nil {
		return application.ErrSequenceExportInvalid
	}
	return nil
}

func validateSequenceExportReplayRecord(record application.ReplaySequenceExportRequestRecord) error {
	if record.ProjectID.IsZero() || record.Owner.Validate(record.Actor, record.RunID, record.TurnID) != nil ||
		(record.Command != "start" && record.Command != "cancel" && record.Command != "delete-artifact") ||
		record.RequestDigest == "" ||
		!json.Valid(record.RequestCanonical) {
		return application.ErrSequenceExportInvalid
	}
	if _, err := domain.ParseRequestID(record.RequestID.String()); err != nil {
		return application.ErrSequenceExportInvalid
	}
	if _, err := domain.ParseDigest(record.RequestDigest.String()); err != nil {
		return application.ErrSequenceExportInvalid
	}
	return nil
}

func sequenceExportOptionalID(value string) any {
	if value == "00000000-0000-0000-0000-000000000000" || value == "" {
		return nil
	}
	return value
}

func resolveSequenceExportTailID(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	jobID domain.WorkJobID,
) (domain.WorkJobID, error) {
	var value string
	err := tx.QueryRowContext(ctx, `
WITH RECURSIVE chain(id) AS (
  SELECT job.id FROM work_jobs job
  JOIN sequence_export_job_details detail ON detail.job_id = job.id
  WHERE job.id = ? AND job.kind = 'sequence-export' AND job.project_id = ?
  UNION
  SELECT retry.id FROM work_jobs retry
  JOIN chain predecessor ON retry.retry_of_job_id = predecessor.id
  JOIN sequence_export_job_details detail ON detail.job_id = retry.id
  WHERE retry.kind = 'sequence-export' AND retry.project_id = ?
)
SELECT job.id FROM chain JOIN work_jobs job ON job.id = chain.id
WHERE NOT EXISTS (SELECT 1 FROM work_jobs retry WHERE retry.retry_of_job_id = job.id)
LIMIT 1`, jobID.String(), projectID.String(), projectID.String()).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkJobID{}, application.ErrSequenceExportNotFound
	}
	if err != nil {
		return domain.WorkJobID{}, err
	}
	result, err := domain.ParseWorkJobID(value)
	if err != nil {
		return domain.WorkJobID{}, application.ErrSequenceExportInvalid
	}
	return result, nil
}

func loadSequenceExportResult(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	jobID domain.WorkJobID,
) (application.SequenceExportResult, error) {
	var state, parametersJSON, sequenceValue, createdValue, updatedValue, rootValue string
	var retryValue, terminalValue, planValue, artifactValue sql.NullString
	var sequenceRevision uint64
	var progress uint16
	err := tx.QueryRowContext(ctx, `
WITH RECURSIVE ancestors(id, retry_of_job_id) AS (
  SELECT id, retry_of_job_id FROM work_jobs WHERE id = ?
  UNION ALL
  SELECT parent.id, parent.retry_of_job_id FROM work_jobs parent
  JOIN ancestors child ON child.retry_of_job_id = parent.id
)
SELECT job.state, job.progress_basis_points, job.parameters_json, job.retry_of_job_id,
       job.terminal_error_code, job.created_at, job.updated_at,
       detail.sequence_id, detail.sequence_revision, detail.render_plan_digest,
       detail.result_artifact_id,
       (SELECT id FROM ancestors WHERE retry_of_job_id IS NULL LIMIT 1)
FROM work_jobs job JOIN sequence_export_job_details detail ON detail.job_id = job.id
WHERE job.id = ? AND job.project_id = ? AND job.kind = 'sequence-export'`,
		jobID.String(), jobID.String(), projectID.String(),
	).Scan(&state, &progress, &parametersJSON, &retryValue, &terminalValue,
		&createdValue, &updatedValue, &sequenceValue, &sequenceRevision, &planValue,
		&artifactValue, &rootValue)
	if errors.Is(err, sql.ErrNoRows) {
		return application.SequenceExportResult{}, application.ErrSequenceExportNotFound
	}
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	parameters, err := application.DecodeSequenceExportJobParameters([]byte(parametersJSON))
	if err != nil || parameters.ProjectID != projectID || parameters.SequenceID.String() != sequenceValue ||
		parameters.SequenceRevision.Value() != sequenceRevision {
		return application.SequenceExportResult{}, application.ErrSequenceExportInvalid
	}
	rootID, err := domain.ParseWorkJobID(rootValue)
	if err != nil {
		return application.SequenceExportResult{}, application.ErrSequenceExportInvalid
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdValue)
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedValue)
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	job := application.SequenceExportJob{
		ID: jobID, RootJobID: rootID, State: domain.WorkJobState(state),
		ProgressBasisPoints: progress, CreatedAt: createdAt.UTC(), UpdatedAt: updatedAt.UTC(),
	}
	if retryValue.Valid {
		retryID, parseErr := domain.ParseWorkJobID(retryValue.String)
		if parseErr != nil {
			return application.SequenceExportResult{}, parseErr
		}
		job.RetryOfJobID = &retryID
	}
	if terminalValue.Valid {
		job.TerminalErrorCode = &terminalValue.String
	}
	if planValue.Valid {
		digest, parseErr := domain.ParseDigest(planValue.String)
		if parseErr != nil {
			return application.SequenceExportResult{}, parseErr
		}
		job.RenderPlanDigest = &digest
	}
	if artifactValue.Valid {
		artifactID, parseErr := domain.ParseArtifactID(artifactValue.String)
		if parseErr != nil {
			return application.SequenceExportResult{}, parseErr
		}
		artifact, loadErr := loadSequenceExportArtifactSummary(ctx, tx, artifactID)
		if loadErr != nil {
			return application.SequenceExportResult{}, loadErr
		}
		job.Artifact = &artifact
	}
	revision, _ := domain.NewRevision(sequenceRevision)
	result := application.SequenceExportResult{
		ProjectID: projectID, SequenceID: parameters.SequenceID, SequenceRevision: revision,
		Preset: parameters.Preset, Job: job,
	}
	result.Recovery = application.SequenceExportRecoveryAction(job)
	result.ActivityCursor, err = loadActivityHead(ctx, tx, "project", projectID.String())
	if err != nil {
		return application.SequenceExportResult{}, err
	}
	return result, nil
}

func loadSequenceExportArtifactSummary(
	ctx context.Context,
	tx *sql.Tx,
	artifactID domain.ArtifactID,
) (domain.SequenceExportArtifactSummary, error) {
	var producer, project, sequence, plan, renderer, targetValue, profile, state, factsJSON, content string
	var revision, size uint64
	if err := tx.QueryRowContext(ctx, `
SELECT producer_job_id, project_id, sequence_id, sequence_revision, render_plan_digest,
       renderer_version, renderer_target, output_profile, state, facts_json, byte_size, content_digest
FROM sequence_export_artifacts WHERE id = ?`, artifactID.String()).Scan(
		&producer, &project, &sequence, &revision, &plan, &renderer, &targetValue,
		&profile, &state, &factsJSON, &size, &content,
	); err != nil {
		return domain.SequenceExportArtifactSummary{}, err
	}
	producerID, producerErr := domain.ParseWorkJobID(producer)
	projectID, projectErr := domain.ParseProjectID(project)
	sequenceID, sequenceErr := domain.ParseSequenceID(sequence)
	revisionValue, revisionErr := domain.NewRevision(revision)
	planDigest, planErr := domain.ParseDigest(plan)
	contentDigest, contentErr := domain.ParseDigest(content)
	byteSize, sizeErr := domain.NewUInt64(size)
	var facts domain.RenderedMediaFacts
	factsErr := json.Unmarshal([]byte(factsJSON), &facts)
	if producerErr != nil || projectErr != nil || sequenceErr != nil || revisionErr != nil ||
		planErr != nil || contentErr != nil || sizeErr != nil || factsErr != nil ||
		application.ValidateSequenceExportFacts(facts) != nil || profile != domain.SequenceExportProfileV1 ||
		(domain.SequenceExportArtifactState(state) != domain.SequenceExportArtifactValid &&
			domain.SequenceExportArtifactState(state) != domain.SequenceExportArtifactInvalid &&
			domain.SequenceExportArtifactState(state) != domain.SequenceExportArtifactDeleted) {
		return domain.SequenceExportArtifactSummary{}, application.ErrSequenceExportInvalid
	}
	return domain.SequenceExportArtifactSummary{
		ID: artifactID, ProducerJobID: producerID, ProjectID: projectID, SequenceID: sequenceID,
		SequenceRevision: revisionValue, RenderPlanDigest: planDigest,
		RendererVersion: renderer, RendererTarget: targetValue, Profile: profile,
		State: domain.SequenceExportArtifactState(state), Facts: facts,
		ByteSize: byteSize, ContentDigest: contentDigest,
	}, nil
}

func appendSequenceExportActivity(
	ctx context.Context,
	tx *sql.Tx,
	projectID domain.ProjectID,
	runID domain.RunID,
	turnID domain.TurnID,
	actor domain.ActorRef,
	jobID domain.WorkJobID,
	eventID domain.ActivityEventID,
	kind string,
	summary string,
	at time.Time,
) error {
	var revisionValue uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM projects WHERE id = ?`, projectID.String()).Scan(&revisionValue); err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		ChangedEntityRefs []application.ChangedEntityRef `json:"changedEntityRefs"`
		JobID             domain.WorkJobID               `json:"jobId"`
		RunID             string                         `json:"runId,omitempty"`
		TurnID            string                         `json:"turnId,omitempty"`
	}{[]application.ChangedEntityRef{}, jobID, runID.String(), turnID.String()})
	if err != nil {
		return err
	}
	_, err = appendActivity(ctx, tx, activityRecord{
		ScopeKind: "project", ScopeID: projectID.String(), EventID: eventID.String(),
		Kind: kind, OccurredAt: formatInstant(at.UTC()), ActorKind: string(actor.Kind), ActorID: actor.IDString(),
		ProjectID: projectID.String(), ProjectRevision: int64(revisionValue),
		OutcomeKind: "work-job", OutcomeID: jobID.String(), SummaryCode: summary, Payload: payload,
	})
	return err
}
