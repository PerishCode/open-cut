package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	SequenceExportJobParametersSchema = "open-cut/sequence-export-job-parameters/v1"
	SequenceExportRequestSchema       = "open-cut/sequence-export-start/v1"
	SequenceExportCancelSchema        = "open-cut/sequence-export-cancel/v1"
	SequenceExportResolverV1          = "sequence-export-input-resolver-v1"
	SequenceExportRendererV1          = "open-cut-render-export-v1"
)

var (
	ErrSequenceExportInvalid       = errors.New("sequence export request is invalid")
	ErrSequenceExportIntegrity     = errors.New("sequence export artifact integrity failed")
	ErrSequenceExportArtifactInUse = errors.New("sequence export artifact is in use")
	ErrSequenceExportUnavailable   = errors.New("sequence export runtime is unavailable")
	ErrSequenceExportNotFound      = errors.New("sequence export was not found")
	ErrSequenceExportRecovery      = errors.New("sequence export cannot use the requested recovery")
	ErrSequenceExportReused        = errors.New("sequence export request identity was reused")
)

type SequenceRenderInputRequirement = SequencePreviewInputRequirement
type SequenceRenderResourcePin = SequencePreviewResourcePin

type SequenceExportStartInput struct {
	RequestID        domain.RequestID `json:"requestId" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	SequenceRevision domain.Revision  `json:"sequenceRevision" format:"uint64-decimal"`
	Preset           string           `json:"preset" enum:"webm-vp9-opus-v1"`
}

func (input SequenceExportStartInput) Validate() error {
	if _, err := domain.ParseRequestID(input.RequestID.String()); err != nil ||
		input.SequenceRevision.Value() == 0 || input.Preset != domain.SequenceExportProfileV1 {
		return ErrSequenceExportInvalid
	}
	return nil
}

type SequenceExportShowInput struct {
	JobID domain.WorkJobID `json:"jobId"`
}

type SequenceExportRetryInput = SequenceExportShowInput

type SequenceExportCancelInput struct {
	RequestID domain.RequestID `json:"requestId" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	JobID     domain.WorkJobID `json:"jobId"`
}

type SequenceExportJobParameters struct {
	ProjectID        domain.ProjectID                 `json:"projectId"`
	SequenceID       domain.SequenceID                `json:"sequenceId"`
	SequenceRevision domain.Revision                  `json:"sequenceRevision"`
	Preset           string                           `json:"preset"`
	ResolverVersion  string                           `json:"resolverVersion"`
	CompilerVersion  string                           `json:"compilerVersion"`
	RendererVersion  string                           `json:"rendererVersion"`
	RendererTarget   string                           `json:"rendererTarget"`
	Inputs           []SequenceRenderInputRequirement `json:"inputs"`
	Resources        []SequenceRenderResourcePin      `json:"resources"`
}

func (parameters SequenceExportJobParameters) Validate() error {
	if parameters.ProjectID.IsZero() || parameters.SequenceID.IsZero() ||
		parameters.SequenceRevision.Value() == 0 || parameters.Preset != domain.SequenceExportProfileV1 ||
		parameters.ResolverVersion != SequenceExportResolverV1 ||
		parameters.CompilerVersion != domain.RenderPlanCompilerV4 || parameters.RendererVersion == "" ||
		len(parameters.RendererVersion) > 1024 || !validPreviewTarget(parameters.RendererTarget) ||
		len(parameters.Inputs) > MaximumRenderPlanItems || len(parameters.Resources) > 32 ||
		(len(parameters.Inputs) == 0 && len(parameters.Resources) == 0) {
		return ErrSequenceExportInvalid
	}
	previousClip := ""
	for _, input := range parameters.Inputs {
		if input.ClipID.IsZero() || input.SourceStreamID.IsZero() || input.ProducerJobID.IsZero() ||
			input.ClipID.String() <= previousClip {
			return ErrSequenceExportInvalid
		}
		previousClip = input.ClipID.String()
	}
	previousResource := ""
	for _, resource := range parameters.Resources {
		key := resource.Kind + "\x00" + resource.ID
		if resource.Kind != "font-bundle" || !validPreviewText(resource.ID, 256) ||
			!validPreviewText(resource.Version, 128) || key <= previousResource {
			return ErrSequenceExportInvalid
		}
		if _, err := domain.ParseDigest(resource.SHA256.String()); err != nil {
			return ErrSequenceExportInvalid
		}
		previousResource = key
	}
	return nil
}

func CanonicalSequenceExportJobParameters(
	parameters SequenceExportJobParameters,
) ([]byte, domain.Digest, SequenceExportJobParameters, error) {
	parameters.Inputs = append([]SequenceRenderInputRequirement(nil), parameters.Inputs...)
	parameters.Resources = append([]SequenceRenderResourcePin(nil), parameters.Resources...)
	sort.Slice(parameters.Inputs, func(left, right int) bool {
		return parameters.Inputs[left].ClipID.String() < parameters.Inputs[right].ClipID.String()
	})
	sort.Slice(parameters.Resources, func(left, right int) bool {
		if parameters.Resources[left].Kind != parameters.Resources[right].Kind {
			return parameters.Resources[left].Kind < parameters.Resources[right].Kind
		}
		return parameters.Resources[left].ID < parameters.Resources[right].ID
	})
	if err := parameters.Validate(); err != nil {
		return nil, "", SequenceExportJobParameters{}, err
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/sequence-export-job-parameters", SequenceExportJobParametersSchema, parameters,
	)
	return canonical, digest, parameters, err
}

func DecodeSequenceExportJobParameters(data []byte) (SequenceExportJobParameters, error) {
	var envelope struct {
		Domain  string                      `json:"domain"`
		Payload SequenceExportJobParameters `json:"payload"`
		Schema  string                      `json:"schema"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		envelope.Domain != "open-cut/sequence-export-job-parameters" ||
		envelope.Schema != SequenceExportJobParametersSchema || envelope.Payload.Validate() != nil {
		return SequenceExportJobParameters{}, ErrSequenceExportInvalid
	}
	return envelope.Payload, nil
}

type SequenceExportJob struct {
	ID                  domain.WorkJobID                      `json:"id"`
	RootJobID           domain.WorkJobID                      `json:"rootJobId"`
	RetryOfJobID        *domain.WorkJobID                     `json:"retryOfJobId,omitempty"`
	State               domain.WorkJobState                   `json:"state" enum:"blocked,queued,running,succeeded,failed,cancelled"`
	ProgressBasisPoints uint16                                `json:"progressBasisPoints" minimum:"0" maximum:"10000"`
	TerminalErrorCode   *string                               `json:"terminalErrorCode,omitempty"`
	RenderPlanDigest    *domain.Digest                        `json:"renderPlanDigest,omitempty"`
	Artifact            *domain.SequenceExportArtifactSummary `json:"artifact,omitempty"`
	CreatedAt           time.Time                             `json:"createdAt"`
	UpdatedAt           time.Time                             `json:"updatedAt"`
}

type SequenceExportJobClaim struct {
	ProjectID        domain.ProjectID
	SequenceID       domain.SequenceID
	SequenceRevision domain.Revision
	Parameters       SequenceExportJobParameters
	ParametersDigest domain.Digest
	ParametersJSON   []byte
}

type SequenceExportResult struct {
	ProjectID        domain.ProjectID    `json:"projectId"`
	SequenceID       domain.SequenceID   `json:"sequenceId"`
	SequenceRevision domain.Revision     `json:"sequenceRevision"`
	Preset           string              `json:"preset" enum:"webm-vp9-opus-v1"`
	Job              SequenceExportJob   `json:"job"`
	Recovery         MediaRecoveryAction `json:"recovery" enum:"retry-job,relink-source,acquire-resource,adopt-revision,update-runtime,none"`
	Replayed         bool                `json:"replayed"`
	ActivityCursor   domain.Cursor       `json:"activityCursor" format:"uint64-decimal"`
}

type RenderMaterialCandidate struct {
	ProducerJobID domain.WorkJobID
	Artifact      domain.ArtifactSummary
	Material      RenderMaterial
}

type SequenceExportPreparationSnapshot struct {
	ProjectID               domain.ProjectID
	ObservedProjectRevision domain.Revision
	Sequence                domain.Sequence
	Clips                   []domain.ClipState
	Captions                []domain.CaptionState
	Assets                  map[string]RenderAssetSnapshot
	Streams                 map[string]domain.SourceStream
	Candidates              map[string][]RenderMaterialCandidate
}

type RequestSequenceExportRecord struct {
	JobID            domain.WorkJobID
	ProjectID        domain.ProjectID
	SequenceID       domain.SequenceID
	RunID            domain.RunID
	TurnID           domain.TurnID
	Actor            domain.ActorRef
	Owner            SequenceExportOwner
	RequestID        domain.RequestID
	RequestDigest    domain.Digest
	RequestCanonical []byte
	Parameters       SequenceExportJobParameters
	ParametersDigest domain.Digest
	ParametersJSON   []byte
	RenderIntent     SequenceRenderIntent
	IntentDigest     domain.Digest
	IntentJSON       []byte
	LogicalKey       string
	ActivityEventID  domain.ActivityEventID
	RequestedAt      time.Time
}

type ReadSequenceExportRecord struct {
	ProjectID domain.ProjectID
	RunID     domain.RunID
	TurnID    domain.TurnID
	Actor     domain.ActorRef
	Owner     SequenceExportOwner
	JobID     domain.WorkJobID
}

type ReplaySequenceExportRequestRecord struct {
	ReadSequenceExportRecord
	Command          string
	RequestID        domain.RequestID
	RequestDigest    domain.Digest
	RequestCanonical []byte
}

type SequenceExportRetrySeed struct {
	Result       SequenceExportResult
	Parameters   SequenceExportJobParameters
	RenderIntent SequenceRenderIntent
}

type SequenceExportRetryPreparation struct {
	ProjectID         domain.ProjectID
	Assets            map[string]RenderAssetSnapshot
	Streams           map[string]domain.SourceStream
	ReusableProducers map[string]bool
}

type RetrySequenceExportRecord struct {
	PredecessorJobID domain.WorkJobID
	Job              RequestSequenceExportRecord
}

type CancelSequenceExportRecord struct {
	ReadSequenceExportRecord
	RequestID        domain.RequestID
	RequestDigest    domain.Digest
	RequestCanonical []byte
	ActivityEventID  domain.ActivityEventID
	CancelledAt      time.Time
}

type SequenceExportRepository interface {
	LoadSequenceExportPreparation(context.Context, domain.ProjectID, domain.SequenceID, domain.Revision) (SequenceExportPreparationSnapshot, error)
	EnsureExplicitRenderInputJob(context.Context, EnsureExplicitRenderInputJobRecord) (domain.WorkJobID, error)
	ReplaySequenceExportRequest(context.Context, ReplaySequenceExportRequestRecord) (SequenceExportResult, bool, error)
	RequestSequenceExport(context.Context, RequestSequenceExportRecord) (SequenceExportResult, error)
	ReadSequenceExport(context.Context, ReadSequenceExportRecord) (SequenceExportResult, error)
	LoadSequenceExportRetrySeed(context.Context, ReadSequenceExportRecord) (SequenceExportRetrySeed, error)
	LoadSequenceExportRetryPreparation(context.Context, SequenceExportRetrySeed) (SequenceExportRetryPreparation, error)
	RetrySequenceExport(context.Context, RetrySequenceExportRecord) (SequenceExportResult, error)
	CancelSequenceExport(context.Context, CancelSequenceExportRecord) (SequenceExportResult, error)
	ListSequenceExportHistory(context.Context, SequenceExportHistoryQuery) (SequenceExportHistoryPage, error)
	DeleteSequenceExportArtifact(context.Context, DeleteSequenceExportArtifactRecord) (SequenceExportResult, error)
}

type SequenceExportSettings struct {
	RendererVersion string
	RendererTarget  string
	FontResource    *domain.RenderFontResource
}

type SequenceExports struct {
	repository SequenceExportRepository
	identities IdentityGenerator
	clock      Clock
	settings   SequenceExportSettings
	available  bool
}

func NewSequenceExports(
	repository SequenceExportRepository,
	identities IdentityGenerator,
	clock Clock,
	settings SequenceExportSettings,
) (*SequenceExports, error) {
	if repository == nil || identities == nil || clock == nil {
		return nil, fmt.Errorf("sequence export dependencies or settings are invalid")
	}
	available := settings.RendererVersion != "" || settings.RendererTarget != "" || settings.FontResource != nil
	if available && (settings.RendererVersion == "" || len(settings.RendererVersion) > 1024 ||
		!validPreviewTarget(settings.RendererTarget) ||
		(settings.FontResource != nil && !validRenderFont(*settings.FontResource))) {
		return nil, fmt.Errorf("sequence export dependencies or settings are invalid")
	}
	return &SequenceExports{
		repository: repository, identities: identities, clock: clock, settings: settings, available: available,
	}, nil
}

func (exports *SequenceExports) Start(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	runID domain.RunID,
	turnID domain.TurnID,
	input SequenceExportStartInput,
) (SequenceExportResult, error) {
	authority, err := productCLIAuthority(ctx)
	if err != nil {
		return SequenceExportResult{}, err
	}
	if projectID.IsZero() || sequenceID.IsZero() || runID.IsZero() || turnID.IsZero() || input.Validate() != nil {
		return SequenceExportResult{}, ErrSequenceExportInvalid
	}
	return exports.start(ctx, projectID, sequenceID, input, authority.Actor, SequenceExportOwner{
		Kind: SequenceExportOwnerAgentRun, ID: runID.String(),
	}, runID, turnID)
}

func (exports *SequenceExports) start(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	input SequenceExportStartInput,
	actor domain.ActorRef,
	owner SequenceExportOwner,
	runID domain.RunID,
	turnID domain.TurnID,
) (SequenceExportResult, error) {
	if !exports.available {
		return SequenceExportResult{}, ErrSequenceExportUnavailable
	}
	if owner.Validate(actor, runID, turnID) != nil {
		return SequenceExportResult{}, fmt.Errorf("%w: owner", ErrSequenceExportInvalid)
	}
	requestCanonical, requestDigest, err := domain.CanonicalDigest(
		"open-cut/sequence-export-start", SequenceExportRequestSchema, struct {
			SequenceRevision domain.Revision `json:"sequenceRevision"`
			Preset           string          `json:"preset"`
		}{input.SequenceRevision, input.Preset},
	)
	if err != nil {
		return SequenceExportResult{}, err
	}
	if replay, found, replayErr := exports.repository.ReplaySequenceExportRequest(
		ctx, ReplaySequenceExportRequestRecord{
			ReadSequenceExportRecord: ReadSequenceExportRecord{
				ProjectID: projectID, RunID: runID, TurnID: turnID, Actor: actor, Owner: owner,
			},
			Command: "start", RequestID: input.RequestID, RequestDigest: requestDigest,
			RequestCanonical: requestCanonical,
		},
	); replayErr != nil {
		return SequenceExportResult{}, replayErr
	} else if found {
		replay.Replayed = true
		return replay, nil
	}
	snapshot, err := exports.repository.LoadSequenceExportPreparation(
		ctx, projectID, sequenceID, input.SequenceRevision,
	)
	if err != nil {
		return SequenceExportResult{}, err
	}
	clips := activeSequencePreviewClips(snapshot.Clips)
	captions := activeSequencePreviewCaptions(snapshot.Captions)
	if len(clips) == 0 && len(captions) == 0 {
		return SequenceExportResult{}, ErrSequenceExportInvalid
	}
	resources, err := exports.resources(captions)
	if err != nil {
		return SequenceExportResult{}, err
	}
	now := exports.clock.Now().UTC()
	inputs, err := exports.resolveInputs(ctx, snapshot, clips, now)
	if err != nil {
		return SequenceExportResult{}, err
	}
	parameters := SequenceExportJobParameters{
		ProjectID: projectID, SequenceID: sequenceID, SequenceRevision: input.SequenceRevision,
		Preset: input.Preset, ResolverVersion: SequenceExportResolverV1,
		CompilerVersion: domain.RenderPlanCompilerV4, RendererVersion: exports.settings.RendererVersion,
		RendererTarget: exports.settings.RendererTarget, Inputs: inputs, Resources: resources,
	}
	parametersJSON, parametersDigest, parameters, err := CanonicalSequenceExportJobParameters(parameters)
	if err != nil {
		return SequenceExportResult{}, err
	}
	intentSnapshot := SequencePreviewPreparationSnapshot{
		ProjectID: snapshot.ProjectID, ObservedProjectRevision: snapshot.ObservedProjectRevision,
		Sequence: snapshot.Sequence, Clips: snapshot.Clips, Captions: snapshot.Captions, Assets: snapshot.Assets,
	}
	intent, intentJSON, intentDigest, err := NewSequenceRenderIntent(intentSnapshot, inputs)
	if err != nil {
		return SequenceExportResult{}, err
	}
	jobID, eventID, err := exports.newJobAndEventIDs(ctx, now)
	if err != nil {
		return SequenceExportResult{}, err
	}
	return exports.repository.RequestSequenceExport(ctx, RequestSequenceExportRecord{
		JobID: jobID, ProjectID: projectID, SequenceID: sequenceID, RunID: runID, TurnID: turnID,
		Actor: actor, Owner: owner, RequestID: input.RequestID, RequestDigest: requestDigest,
		RequestCanonical: requestCanonical, Parameters: parameters, ParametersDigest: parametersDigest,
		ParametersJSON: parametersJSON, RenderIntent: intent, IntentDigest: intentDigest, IntentJSON: intentJSON,
		LogicalKey: "sequence-export/v1/" + jobID.String(), ActivityEventID: eventID, RequestedAt: now,
	})
}

func (exports *SequenceExports) Show(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	turnID domain.TurnID,
	input SequenceExportShowInput,
) (SequenceExportResult, error) {
	authority, err := productCLIAuthority(ctx)
	if err != nil {
		return SequenceExportResult{}, err
	}
	if projectID.IsZero() || runID.IsZero() || turnID.IsZero() || input.JobID.IsZero() {
		return SequenceExportResult{}, ErrSequenceExportInvalid
	}
	return exports.repository.ReadSequenceExport(ctx, ReadSequenceExportRecord{
		ProjectID: projectID, RunID: runID, TurnID: turnID, Actor: authority.Actor,
		Owner: SequenceExportOwner{Kind: SequenceExportOwnerAgentRun, ID: runID.String()}, JobID: input.JobID,
	})
}

func (exports *SequenceExports) Retry(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	turnID domain.TurnID,
	input SequenceExportRetryInput,
) (SequenceExportResult, error) {
	authority, err := productCLIAuthority(ctx)
	if err != nil {
		return SequenceExportResult{}, err
	}
	read := ReadSequenceExportRecord{
		ProjectID: projectID, RunID: runID, TurnID: turnID, Actor: authority.Actor,
		Owner: SequenceExportOwner{Kind: SequenceExportOwnerAgentRun, ID: runID.String()}, JobID: input.JobID,
	}
	if projectID.IsZero() || runID.IsZero() || turnID.IsZero() || input.JobID.IsZero() {
		return SequenceExportResult{}, ErrSequenceExportInvalid
	}
	return exports.retry(ctx, read)
}

func (exports *SequenceExports) retry(
	ctx context.Context,
	read ReadSequenceExportRecord,
) (SequenceExportResult, error) {
	if !exports.available {
		return SequenceExportResult{}, ErrSequenceExportUnavailable
	}
	seed, err := exports.repository.LoadSequenceExportRetrySeed(ctx, read)
	if err != nil {
		return SequenceExportResult{}, err
	}
	if seed.Result.Recovery != MediaRecoveryRetryJob || seed.Result.Job.ID.IsZero() {
		return SequenceExportResult{}, ErrSequenceExportRecovery
	}
	now := exports.clock.Now().UTC()
	preparation, err := exports.repository.LoadSequenceExportRetryPreparation(ctx, seed)
	if err != nil {
		return SequenceExportResult{}, err
	}
	parameters := seed.Parameters
	clips := make(map[string]SequencePreviewIntentClip, len(seed.RenderIntent.Clips))
	for _, clip := range seed.RenderIntent.Clips {
		clips[clip.ID.String()] = clip
	}
	for index, inputRequirement := range parameters.Inputs {
		if preparation.ReusableProducers[inputRequirement.ProducerJobID.String()] {
			continue
		}
		clip, exists := clips[inputRequirement.ClipID.String()]
		if !exists || clip.SourceStreamID != inputRequirement.SourceStreamID {
			return SequenceExportResult{}, ErrSequenceExportInvalid
		}
		producer, ensureErr := exports.ensureRenderInput(ctx, SequenceExportPreparationSnapshot{
			ProjectID: preparation.ProjectID, Assets: preparation.Assets, Streams: preparation.Streams,
		}, clip.state(seed.RenderIntent.SequenceID), now)
		if ensureErr != nil {
			return SequenceExportResult{}, ensureErr
		}
		parameters.Inputs[index].ProducerJobID = producer
	}
	parameters.RendererVersion = exports.settings.RendererVersion
	parameters.RendererTarget = exports.settings.RendererTarget
	parametersJSON, parametersDigest, parameters, err := CanonicalSequenceExportJobParameters(parameters)
	if err != nil {
		return SequenceExportResult{}, err
	}
	intent, intentJSON, intentDigest, err := CanonicalSequenceRenderIntent(seed.RenderIntent, parameters.Inputs)
	if err != nil {
		return SequenceExportResult{}, err
	}
	jobID, eventID, err := exports.newJobAndEventIDs(ctx, now)
	if err != nil {
		return SequenceExportResult{}, err
	}
	return exports.repository.RetrySequenceExport(ctx, RetrySequenceExportRecord{
		PredecessorJobID: seed.Result.Job.ID,
		Job: RequestSequenceExportRecord{
			JobID: jobID, ProjectID: read.ProjectID, SequenceID: parameters.SequenceID,
			RunID: read.RunID, TurnID: read.TurnID, Actor: read.Actor, Owner: read.Owner,
			Parameters: parameters, ParametersDigest: parametersDigest, ParametersJSON: parametersJSON,
			RenderIntent: intent, IntentDigest: intentDigest, IntentJSON: intentJSON,
			LogicalKey:      "sequence-export/v1/" + jobID.String(),
			ActivityEventID: eventID, RequestedAt: now,
		},
	})
}

func (exports *SequenceExports) Cancel(
	ctx context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	turnID domain.TurnID,
	input SequenceExportCancelInput,
) (SequenceExportResult, error) {
	authority, err := productCLIAuthority(ctx)
	if err != nil {
		return SequenceExportResult{}, err
	}
	if projectID.IsZero() || runID.IsZero() || turnID.IsZero() || input.JobID.IsZero() {
		return SequenceExportResult{}, ErrSequenceExportInvalid
	}
	return exports.cancel(ctx, ReadSequenceExportRecord{
		ProjectID: projectID, RunID: runID, TurnID: turnID, Actor: authority.Actor,
		Owner: SequenceExportOwner{Kind: SequenceExportOwnerAgentRun, ID: runID.String()}, JobID: input.JobID,
	}, input.RequestID)
}

func (exports *SequenceExports) cancel(
	ctx context.Context,
	read ReadSequenceExportRecord,
	requestID domain.RequestID,
) (SequenceExportResult, error) {
	if read.Owner.Validate(read.Actor, read.RunID, read.TurnID) != nil {
		return SequenceExportResult{}, ErrSequenceExportInvalid
	}
	if _, err := domain.ParseRequestID(requestID.String()); err != nil {
		return SequenceExportResult{}, ErrSequenceExportInvalid
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/sequence-export-cancel", SequenceExportCancelSchema, struct {
			JobID domain.WorkJobID `json:"jobId"`
		}{read.JobID},
	)
	if err != nil {
		return SequenceExportResult{}, err
	}
	now := exports.clock.Now().UTC()
	eventID, err := exports.newActivityEventID(ctx, now)
	if err != nil {
		return SequenceExportResult{}, err
	}
	return exports.repository.CancelSequenceExport(ctx, CancelSequenceExportRecord{
		ReadSequenceExportRecord: read,
		RequestID:                requestID, RequestDigest: digest, RequestCanonical: canonical,
		ActivityEventID: eventID, CancelledAt: now,
	})
}

func SequenceExportRecoveryAction(job SequenceExportJob) MediaRecoveryAction {
	if job.Artifact != nil && (job.Artifact.State == domain.SequenceExportArtifactInvalid ||
		job.Artifact.State == domain.SequenceExportArtifactDeleted) {
		return MediaRecoveryRetryJob
	}
	if job.State == domain.MediaJobCancelled {
		return MediaRecoveryRetryJob
	}
	if job.State != domain.MediaJobFailed || job.TerminalErrorCode == nil {
		return MediaRecoveryNone
	}
	switch *job.TerminalErrorCode {
	case "renderer-failed", "attempt-limit-exceeded", "renderer-output-invalid":
		return MediaRecoveryRetryJob
	case "input-job-failed", "input-artifact-unavailable", "render-input-unavailable":
		return MediaRecoveryRelinkSource
	case "render-font-unavailable":
		return MediaRecoveryAcquireResource
	case "sequence-revision-conflict":
		return MediaRecoveryAdoptRevision
	default:
		return MediaRecoveryUpdateRuntime
	}
}

func (exports *SequenceExports) resources(
	captions []domain.CaptionState,
) ([]SequenceRenderResourcePin, error) {
	if len(captions) == 0 {
		return []SequenceRenderResourcePin{}, nil
	}
	if exports.settings.FontResource == nil {
		return nil, ErrRenderFontRequired
	}
	font := *exports.settings.FontResource
	return []SequenceRenderResourcePin{{
		Kind: "font-bundle", ID: font.ResourceID, Version: font.Version, SHA256: font.SHA256,
	}}, nil
}

func (exports *SequenceExports) resolveInputs(
	ctx context.Context,
	snapshot SequenceExportPreparationSnapshot,
	clips []domain.ClipState,
	at time.Time,
) ([]SequenceRenderInputRequirement, error) {
	inputs := make([]SequenceRenderInputRequirement, 0, len(clips))
	resolved := make(map[string]domain.WorkJobID)
	for _, clip := range clips {
		var producer domain.WorkJobID
		for _, candidate := range snapshot.Candidates[clip.AssetID.String()] {
			if candidate.Material.ContainsStream(clip.SourceStreamID) {
				producer = candidate.ProducerJobID
				break
			}
		}
		if producer.IsZero() {
			key := clip.AssetID.String() + "/" + clip.SourceStreamID.String()
			producer = resolved[key]
			if producer.IsZero() {
				var err error
				producer, err = exports.ensureRenderInput(ctx, snapshot, clip, at)
				if err != nil {
					return nil, err
				}
				resolved[key] = producer
			}
		}
		inputs = append(inputs, SequenceRenderInputRequirement{
			ClipID: clip.ID, SourceStreamID: clip.SourceStreamID, ProducerJobID: producer,
		})
	}
	return inputs, nil
}

func (exports *SequenceExports) ensureRenderInput(
	ctx context.Context,
	snapshot SequenceExportPreparationSnapshot,
	clip domain.ClipState,
	at time.Time,
) (domain.WorkJobID, error) {
	asset, exists := snapshot.Assets[clip.AssetID.String()]
	stream, streamExists := snapshot.Streams[clip.SourceStreamID.String()]
	if !exists || !streamExists {
		return domain.WorkJobID{}, ErrRenderInputRequired
	}
	selection := SourceProxySelection{Policy: SourceProxySelectionExplicit}
	switch stream.Descriptor.MediaType {
	case domain.MediaVideo:
		selection.VideoStreamID = &stream.ID
	case domain.MediaAudio:
		selection.AudioStreamID = &stream.ID
	default:
		return domain.WorkJobID{}, ErrRenderInputRequired
	}
	parameters := InitialMediaJobParameters{
		AssetID: clip.AssetID, Kind: domain.MediaJobRenderInput, Profile: RenderInputProfile,
		RenderInputSelection: &selection,
	}
	canonical, digest, err := CanonicalInitialMediaJobParameters(parameters)
	if err != nil {
		return domain.WorkJobID{}, err
	}
	jobValue, err := exports.identities.NewID(ctx, at)
	if err != nil {
		return domain.WorkJobID{}, err
	}
	jobID, err := domain.ParseWorkJobID(jobValue)
	if err != nil {
		return domain.WorkJobID{}, err
	}
	return exports.repository.EnsureExplicitRenderInputJob(ctx, EnsureExplicitRenderInputJobRecord{
		JobID: jobID, ProjectID: snapshot.ProjectID, AssetID: clip.AssetID,
		Fingerprint: asset.AcceptedFingerprint, SourceStream: stream, Parameters: parameters,
		Canonical: canonical, Digest: digest, LogicalKey: "media/v1/" + clip.AssetID.String() +
			"/render-input/" + digest.String(), CreatedAt: at,
	})
}

func (exports *SequenceExports) newJobAndEventIDs(
	ctx context.Context,
	at time.Time,
) (domain.WorkJobID, domain.ActivityEventID, error) {
	jobValue, err := exports.identities.NewID(ctx, at)
	if err != nil {
		return domain.WorkJobID{}, domain.ActivityEventID{}, err
	}
	jobID, err := domain.ParseWorkJobID(jobValue)
	if err != nil {
		return domain.WorkJobID{}, domain.ActivityEventID{}, err
	}
	eventValue, err := exports.identities.NewID(ctx, at)
	if err != nil {
		return domain.WorkJobID{}, domain.ActivityEventID{}, err
	}
	eventID, err := domain.ParseActivityEventID(eventValue)
	return jobID, eventID, err
}

func (exports *SequenceExports) newActivityEventID(
	ctx context.Context,
	at time.Time,
) (domain.ActivityEventID, error) {
	eventValue, err := exports.identities.NewID(ctx, at)
	if err != nil {
		return domain.ActivityEventID{}, err
	}
	return domain.ParseActivityEventID(eventValue)
}

func ValidateSequenceExportRequestRecord(record RequestSequenceExportRecord) error {
	if record.JobID.IsZero() || record.ProjectID.IsZero() || record.SequenceID.IsZero() ||
		record.Owner.Validate(record.Actor, record.RunID, record.TurnID) != nil ||
		record.Parameters.Validate() != nil ||
		record.Parameters.ProjectID != record.ProjectID || record.Parameters.SequenceID != record.SequenceID ||
		record.LogicalKey == "" || len(record.LogicalKey) > 1024 || record.ActivityEventID.IsZero() ||
		record.RequestedAt.IsZero() {
		return fmt.Errorf("%w: request envelope", ErrSequenceExportInvalid)
	}
	if _, err := domain.ParseRequestID(record.RequestID.String()); err != nil {
		return fmt.Errorf("%w: request identity", ErrSequenceExportInvalid)
	}
	canonical, digest, normalized, err := CanonicalSequenceExportJobParameters(record.Parameters)
	if err != nil || normalized.Validate() != nil || digest != record.ParametersDigest ||
		!bytes.Equal(canonical, record.ParametersJSON) {
		return fmt.Errorf("%w: job parameters", ErrSequenceExportInvalid)
	}
	intent, intentJSON, intentDigest, err := CanonicalSequenceRenderIntent(record.RenderIntent, normalized.Inputs)
	if err != nil || intent.ProjectID != record.ProjectID || intent.SequenceID != record.SequenceID ||
		intentDigest != record.IntentDigest || !bytes.Equal(intentJSON, record.IntentJSON) {
		return fmt.Errorf("%w: render intent", ErrSequenceExportInvalid)
	}
	requestCanonical, requestDigest, err := domain.CanonicalDigest(
		"open-cut/sequence-export-start", SequenceExportRequestSchema, struct {
			SequenceRevision domain.Revision `json:"sequenceRevision"`
			Preset           string          `json:"preset"`
		}{record.Parameters.SequenceRevision, record.Parameters.Preset},
	)
	if err != nil || requestDigest != record.RequestDigest || !bytes.Equal(requestCanonical, record.RequestCanonical) {
		return fmt.Errorf("%w: canonical request", ErrSequenceExportInvalid)
	}
	return nil
}
