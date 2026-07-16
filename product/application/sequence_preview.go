package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	SequencePreviewJobParametersSchema = "open-cut/sequence-preview-job-parameters/v1"
	SequencePreviewResolverV1          = "sequence-preview-input-resolver-v1"
	SequencePreviewRendererV1          = "open-cut-render-v1"
)

var (
	ErrSequencePreviewInvalid  = errors.New("sequence preview request is invalid")
	ErrSequencePreviewNotFound = errors.New("sequence preview was not found")
	ErrSequencePreviewRecovery = errors.New("sequence preview cannot use the requested recovery")
)

type SequencePreviewPreparationStatus string

const (
	SequencePreviewEmpty     SequencePreviewPreparationStatus = "empty"
	SequencePreviewPreparing SequencePreviewPreparationStatus = "preparing"
	SequencePreviewReady     SequencePreviewPreparationStatus = "ready"
	SequencePreviewFailed    SequencePreviewPreparationStatus = "failed"
)

type SequencePreviewInputRequirement struct {
	ClipID         domain.ClipID         `json:"clipId"`
	SourceStreamID domain.SourceStreamID `json:"sourceStreamId"`
	ProducerJobID  domain.WorkJobID      `json:"producerJobId"`
}

type SequencePreviewResourcePin struct {
	Kind    string        `json:"kind"`
	ID      string        `json:"id"`
	Version string        `json:"version"`
	SHA256  domain.Digest `json:"sha256"`
}

type SequencePreviewJobParameters struct {
	ProjectID        domain.ProjectID                  `json:"projectId"`
	SequenceID       domain.SequenceID                 `json:"sequenceId"`
	SequenceRevision domain.Revision                   `json:"sequenceRevision"`
	ResolverVersion  string                            `json:"resolverVersion"`
	CompilerVersion  string                            `json:"compilerVersion"`
	RendererVersion  string                            `json:"rendererVersion"`
	RendererTarget   string                            `json:"rendererTarget"`
	OutputProfile    string                            `json:"outputProfile"`
	Inputs           []SequencePreviewInputRequirement `json:"inputs"`
	Resources        []SequencePreviewResourcePin      `json:"resources"`
}

type SequencePreviewProxyCandidate struct {
	ProducerJobID domain.WorkJobID
	Artifact      domain.ArtifactSummary
	Manifest      SourceProxyArtifactManifest
}

type SequencePreviewPreparationSnapshot struct {
	ProjectID               domain.ProjectID
	ObservedProjectRevision domain.Revision
	Sequence                domain.Sequence
	Clips                   []domain.ClipState
	Captions                []domain.CaptionState
	Assets                  map[string]RenderAssetSnapshot
	Streams                 map[string]domain.SourceStream
	Candidates              map[string][]SequencePreviewProxyCandidate
}

type EnsureExplicitSourceProxyJobRecord struct {
	JobID         domain.WorkJobID
	ProjectID     domain.ProjectID
	AssetID       domain.AssetID
	Fingerprint   domain.Digest
	SourceStreams []domain.SourceStream
	Parameters    InitialMediaJobParameters
	Canonical     []byte
	Digest        domain.Digest
	LogicalKey    string
	CreatedAt     time.Time
}

type EnsureSequencePreviewJobRecord struct {
	JobID           domain.WorkJobID
	Parameters      SequencePreviewJobParameters
	Canonical       []byte
	Digest          domain.Digest
	RenderIntent    SequencePreviewRenderIntent
	IntentCanonical []byte
	IntentDigest    domain.Digest
	LogicalKey      string
	CreatedAt       time.Time
}

type SequencePreviewJobProjection struct {
	ID                  domain.WorkJobID
	State               domain.WorkJobState
	ProgressBasisPoints uint16
	TerminalErrorCode   *string
	RenderPlanDigest    *domain.Digest
	Artifact            *domain.SequencePreviewArtifactSummary
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type SequencePreviewJobClaim struct {
	ProjectID        domain.ProjectID
	SequenceID       domain.SequenceID
	SequenceRevision domain.Revision
	Parameters       SequencePreviewJobParameters
	ParametersDigest domain.Digest
	ParametersJSON   []byte
}

type SequencePreviewPreparation struct {
	Status SequencePreviewPreparationStatus
	Job    *SequencePreviewJobProjection
}

type RejectSequencePreviewArtifactRecord struct {
	ProjectID        domain.ProjectID
	SequenceID       domain.SequenceID
	SequenceRevision domain.Revision
	ArtifactID       domain.ArtifactID
	JobID            domain.WorkJobID
	RetryJobID       domain.WorkJobID
	EventID          domain.ActivityEventID
	Code             MediaDiagnosticCode
	RejectedAt       time.Time
}

type SequencePreviewRetrySeed struct {
	Job          SequencePreviewJobProjection
	Parameters   SequencePreviewJobParameters
	RenderIntent SequencePreviewRenderIntent
}

type RetrySequencePreviewJobRecord struct {
	PredecessorJobID domain.WorkJobID
	Job              EnsureSequencePreviewJobRecord
	EventID          domain.ActivityEventID
	Actor            domain.ActorRef
}

type SequencePreviewRepository interface {
	LoadSequencePreviewPreparation(
		context.Context,
		domain.ProjectID,
		domain.SequenceID,
		domain.Revision,
	) (SequencePreviewPreparationSnapshot, error)
	EnsureExplicitSourceProxyJob(
		context.Context,
		EnsureExplicitSourceProxyJobRecord,
	) (domain.WorkJobID, error)
	EnsureSequencePreviewJob(
		context.Context,
		EnsureSequencePreviewJobRecord,
	) (SequencePreviewJobProjection, error)
	RejectSequencePreviewArtifact(
		context.Context,
		RejectSequencePreviewArtifactRecord,
	) (SequencePreviewJobProjection, error)
	LoadSequencePreviewContinuation(
		context.Context,
		domain.ProjectID,
		domain.SequenceID,
		domain.Revision,
		domain.WorkJobID,
	) (SequencePreviewJobProjection, error)
	LoadSequencePreviewRetrySeed(
		context.Context,
		domain.ProjectID,
		domain.SequenceID,
		domain.Revision,
		domain.WorkJobID,
	) (SequencePreviewRetrySeed, error)
	RetrySequencePreviewJob(
		context.Context,
		RetrySequencePreviewJobRecord,
	) (SequencePreviewJobProjection, error)
}

type SequencePreviewSettings struct {
	RendererVersion string
	RendererTarget  string
	FontResource    *domain.RenderFontResource
}

type SequencePreviews struct {
	repository SequencePreviewRepository
	identities IdentityGenerator
	clock      Clock
	settings   SequencePreviewSettings
}

func NewSequencePreviews(
	repository SequencePreviewRepository,
	identities IdentityGenerator,
	clock Clock,
	settings SequencePreviewSettings,
) (*SequencePreviews, error) {
	if repository == nil || identities == nil || clock == nil || settings.RendererVersion == "" ||
		len(settings.RendererVersion) > 1024 || !validPreviewTarget(settings.RendererTarget) ||
		(settings.FontResource != nil && !validRenderFont(*settings.FontResource)) {
		return nil, fmt.Errorf("sequence preview dependencies or settings are invalid")
	}
	return &SequencePreviews{
		repository: repository, identities: identities, clock: clock, settings: settings,
	}, nil
}

func (previews *SequencePreviews) Prepare(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	expectedSequenceRevision domain.Revision,
) (SequencePreviewPreparation, error) {
	if _, err := creatorAuthority(ctx); err != nil {
		return SequencePreviewPreparation{}, err
	}
	return previews.prepare(ctx, projectID, sequenceID, expectedSequenceRevision)
}

// PrepareForAgentOperationalRead resolves the same immutable preview graph as
// Creator playback, but authorizes it as a turn-scoped product-CLI dependency.
// It is application plumbing for another CLI command, never an Agent surface.
func (previews *SequencePreviews) PrepareForAgentOperationalRead(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	expectedSequenceRevision domain.Revision,
) (SequencePreviewPreparation, error) {
	if _, err := productCLIAuthority(ctx); err != nil {
		return SequencePreviewPreparation{}, err
	}
	return previews.prepare(ctx, projectID, sequenceID, expectedSequenceRevision)
}

func (previews *SequencePreviews) prepare(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	expectedSequenceRevision domain.Revision,
) (SequencePreviewPreparation, error) {
	if projectID.IsZero() || sequenceID.IsZero() || expectedSequenceRevision.Value() == 0 {
		return SequencePreviewPreparation{}, ErrSequencePreviewInvalid
	}
	snapshot, err := previews.repository.LoadSequencePreviewPreparation(
		ctx, projectID, sequenceID, expectedSequenceRevision,
	)
	if err != nil {
		return SequencePreviewPreparation{}, err
	}
	activeClips := activeSequencePreviewClips(snapshot.Clips)
	activeCaptions := activeSequencePreviewCaptions(snapshot.Captions)
	if len(activeClips) == 0 && len(activeCaptions) == 0 {
		return SequencePreviewPreparation{Status: SequencePreviewEmpty}, nil
	}
	resources := make([]SequencePreviewResourcePin, 0, 1)
	if len(activeCaptions) > 0 {
		if previews.settings.FontResource == nil {
			return SequencePreviewPreparation{}, ErrRenderFontRequired
		}
		font := *previews.settings.FontResource
		resources = append(resources, SequencePreviewResourcePin{
			Kind: "font-bundle", ID: font.ResourceID, Version: font.Version, SHA256: font.SHA256,
		})
	}
	at := previews.clock.Now().UTC()
	inputs := make([]SequencePreviewInputRequirement, 0, len(activeClips))
	resolved := make(map[string]domain.WorkJobID)
	for _, clip := range activeClips {
		producer, exists := selectSequencePreviewCandidate(snapshot.Candidates[clip.AssetID.String()], clip.SourceStreamID)
		if !exists {
			key := clip.AssetID.String() + "/" + clip.SourceStreamID.String()
			producer, exists = resolved[key]
			if !exists {
				producer, err = previews.ensureExplicitProxy(ctx, snapshot, clip, at)
				if err != nil {
					return SequencePreviewPreparation{}, err
				}
				resolved[key] = producer
			}
		}
		inputs = append(inputs, SequencePreviewInputRequirement{
			ClipID: clip.ID, SourceStreamID: clip.SourceStreamID, ProducerJobID: producer,
		})
	}
	parameters := SequencePreviewJobParameters{
		ProjectID: projectID, SequenceID: sequenceID, SequenceRevision: expectedSequenceRevision,
		ResolverVersion: SequencePreviewResolverV1, CompilerVersion: domain.RenderPlanCompilerV4,
		RendererVersion: previews.settings.RendererVersion, RendererTarget: previews.settings.RendererTarget,
		OutputProfile: domain.SequencePreviewProfileV1, Inputs: inputs, Resources: resources,
	}
	canonical, digest, normalized, err := CanonicalSequencePreviewJobParameters(parameters)
	if err != nil {
		return SequencePreviewPreparation{}, err
	}
	intent, intentCanonical, intentDigest, err := NewSequencePreviewRenderIntent(snapshot, normalized.Inputs)
	if err != nil {
		return SequencePreviewPreparation{}, err
	}
	jobID, err := previews.newWorkJobID(ctx, at)
	if err != nil {
		return SequencePreviewPreparation{}, err
	}
	job, err := previews.repository.EnsureSequencePreviewJob(ctx, EnsureSequencePreviewJobRecord{
		JobID: jobID, Parameters: normalized, Canonical: canonical, Digest: digest,
		RenderIntent: intent, IntentCanonical: intentCanonical, IntentDigest: intentDigest,
		LogicalKey: "sequence-preview/v1/" + digest.String() + "/" + intentDigest.String(), CreatedAt: at,
	})
	if err != nil {
		return SequencePreviewPreparation{}, err
	}
	return sequencePreviewPreparationForJob(job)
}

func (previews *SequencePreviews) Continue(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	sequenceRevision domain.Revision,
	jobID domain.WorkJobID,
	expectedRenderPlanDigest *domain.Digest,
) (SequencePreviewPreparation, error) {
	if _, err := creatorAuthority(ctx); err != nil {
		return SequencePreviewPreparation{}, err
	}
	return previews.continuePreview(
		ctx, projectID, sequenceID, sequenceRevision, jobID, expectedRenderPlanDigest,
	)
}

// ContinueForAgentOperationalRead observes the same immutable preview lineage
// as Creator playback. It is only callable from another product-CLI
// application operation and does not create a second Agent surface.
func (previews *SequencePreviews) ContinueForAgentOperationalRead(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	sequenceRevision domain.Revision,
	jobID domain.WorkJobID,
) (SequencePreviewPreparation, error) {
	if _, err := productCLIAuthority(ctx); err != nil {
		return SequencePreviewPreparation{}, err
	}
	return previews.continuePreview(ctx, projectID, sequenceID, sequenceRevision, jobID, nil)
}

func (previews *SequencePreviews) continuePreview(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	sequenceRevision domain.Revision,
	jobID domain.WorkJobID,
	expectedRenderPlanDigest *domain.Digest,
) (SequencePreviewPreparation, error) {
	if projectID.IsZero() || sequenceID.IsZero() || sequenceRevision.Value() == 0 || jobID.IsZero() {
		return SequencePreviewPreparation{}, ErrSequencePreviewInvalid
	}
	job, err := previews.repository.LoadSequencePreviewContinuation(
		ctx, projectID, sequenceID, sequenceRevision, jobID,
	)
	if err != nil {
		return SequencePreviewPreparation{}, err
	}
	if err := validateExpectedSequencePreviewPlan(job, expectedRenderPlanDigest); err != nil {
		return SequencePreviewPreparation{}, err
	}
	return sequencePreviewPreparationForJob(job)
}

func (previews *SequencePreviews) Retry(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	sequenceRevision domain.Revision,
	jobID domain.WorkJobID,
	expectedRenderPlanDigest *domain.Digest,
) (SequencePreviewPreparation, error) {
	authority, err := creatorAuthority(ctx)
	if err != nil {
		return SequencePreviewPreparation{}, err
	}
	return previews.retry(
		ctx, authority, projectID, sequenceID, sequenceRevision, jobID, expectedRenderPlanDigest,
	)
}

// RetryForAgentOperationalRead performs only the preview retry needed by a
// product-CLI operational read. Recovery remains explicit at the enclosing CLI
// command; this method is not independently discoverable or routable.
func (previews *SequencePreviews) RetryForAgentOperationalRead(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	sequenceRevision domain.Revision,
	jobID domain.WorkJobID,
) (SequencePreviewPreparation, error) {
	authority, err := productCLIAuthority(ctx)
	if err != nil {
		return SequencePreviewPreparation{}, err
	}
	return previews.retry(ctx, authority, projectID, sequenceID, sequenceRevision, jobID, nil)
}

func (previews *SequencePreviews) retry(
	ctx context.Context,
	authority Authority,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	sequenceRevision domain.Revision,
	jobID domain.WorkJobID,
	expectedRenderPlanDigest *domain.Digest,
) (SequencePreviewPreparation, error) {
	if projectID.IsZero() || sequenceID.IsZero() || sequenceRevision.Value() == 0 || jobID.IsZero() {
		return SequencePreviewPreparation{}, ErrSequencePreviewInvalid
	}
	tail, err := previews.repository.LoadSequencePreviewContinuation(
		ctx, projectID, sequenceID, sequenceRevision, jobID,
	)
	if err != nil {
		return SequencePreviewPreparation{}, err
	}
	if err := validateExpectedSequencePreviewPlan(tail, expectedRenderPlanDigest); err != nil {
		return SequencePreviewPreparation{}, err
	}
	if tail.ID != jobID {
		return sequencePreviewPreparationForJob(tail)
	}
	if tail.State != domain.MediaJobFailed && tail.State != domain.MediaJobCancelled {
		return SequencePreviewPreparation{}, ErrSequencePreviewRecovery
	}
	if SequencePreviewRecoveryAction(tail) != MediaRecoveryRetryJob {
		return SequencePreviewPreparation{}, ErrSequencePreviewRecovery
	}
	seed, err := previews.repository.LoadSequencePreviewRetrySeed(
		ctx, projectID, sequenceID, sequenceRevision, tail.ID,
	)
	if err != nil {
		return SequencePreviewPreparation{}, err
	}
	if seed.Job.ID != tail.ID || seed.Job.State != tail.State {
		return SequencePreviewPreparation{}, ErrSequencePreviewInvalid
	}
	parameters := seed.Parameters
	parameters.RendererVersion = previews.settings.RendererVersion
	parameters.RendererTarget = previews.settings.RendererTarget
	canonical, digest, normalized, err := CanonicalSequencePreviewJobParameters(parameters)
	if err != nil {
		return SequencePreviewPreparation{}, err
	}
	intent, intentCanonical, intentDigest, err := CanonicalSequencePreviewRenderIntent(
		seed.RenderIntent, normalized.Inputs,
	)
	if err != nil {
		return SequencePreviewPreparation{}, err
	}
	now := previews.clock.Now().UTC()
	retryID, err := previews.newWorkJobID(ctx, now)
	if err != nil {
		return SequencePreviewPreparation{}, err
	}
	eventValue, err := previews.identities.NewID(ctx, now)
	if err != nil {
		return SequencePreviewPreparation{}, err
	}
	eventID, err := domain.ParseActivityEventID(eventValue)
	if err != nil {
		return SequencePreviewPreparation{}, err
	}
	retry, err := previews.repository.RetrySequencePreviewJob(ctx, RetrySequencePreviewJobRecord{
		PredecessorJobID: tail.ID,
		Job: EnsureSequencePreviewJobRecord{
			JobID: retryID, Parameters: normalized, Canonical: canonical, Digest: digest,
			RenderIntent: intent, IntentCanonical: intentCanonical, IntentDigest: intentDigest,
			LogicalKey: "sequence-preview/v1/" + digest.String() + "/" + intentDigest.String(), CreatedAt: now,
		},
		EventID: eventID, Actor: authority.Actor,
	})
	if err != nil {
		return SequencePreviewPreparation{}, err
	}
	return sequencePreviewPreparationForJob(retry)
}

func validateExpectedSequencePreviewPlan(
	job SequencePreviewJobProjection,
	expected *domain.Digest,
) error {
	if expected == nil {
		return nil
	}
	if *expected == "" || job.RenderPlanDigest == nil || *job.RenderPlanDigest != *expected {
		return ErrSequencePreviewInvalid
	}
	return nil
}

func SequencePreviewRecoveryAction(job SequencePreviewJobProjection) MediaRecoveryAction {
	if job.State == domain.MediaJobCancelled {
		return MediaRecoveryRetryJob
	}
	if job.State != domain.MediaJobFailed || job.TerminalErrorCode == nil {
		return MediaRecoveryNone
	}
	switch *job.TerminalErrorCode {
	case "renderer-failed", "attempt-limit-exceeded":
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

func sequencePreviewPreparationForJob(
	job SequencePreviewJobProjection,
) (SequencePreviewPreparation, error) {
	status := SequencePreviewPreparing
	switch job.State {
	case domain.MediaJobBlocked, domain.MediaJobQueued, domain.MediaJobRunning:
		if job.Artifact != nil {
			return SequencePreviewPreparation{}, ErrSequencePreviewInvalid
		}
	case domain.MediaJobSucceeded:
		if job.Artifact == nil || job.RenderPlanDigest == nil ||
			job.Artifact.State != domain.SequencePreviewArtifactReady ||
			job.Artifact.RenderPlanDigest != *job.RenderPlanDigest {
			return SequencePreviewPreparation{}, ErrSequencePreviewInvalid
		}
		status = SequencePreviewReady
	case domain.MediaJobFailed, domain.MediaJobCancelled:
		if job.Artifact != nil {
			return SequencePreviewPreparation{}, ErrSequencePreviewInvalid
		}
		status = SequencePreviewFailed
	default:
		return SequencePreviewPreparation{}, ErrSequencePreviewInvalid
	}
	return SequencePreviewPreparation{Status: status, Job: &job}, nil
}

func (previews *SequencePreviews) RejectArtifact(
	ctx context.Context,
	record RejectSequencePreviewArtifactRecord,
) (SequencePreviewJobProjection, error) {
	if _, err := creatorAuthority(ctx); err != nil {
		return SequencePreviewJobProjection{}, err
	}
	if record.ProjectID.IsZero() || record.SequenceID.IsZero() || record.SequenceRevision.Value() == 0 ||
		record.ArtifactID.IsZero() || record.JobID.IsZero() || record.RetryJobID.IsZero() ||
		record.EventID.IsZero() || record.JobID == record.RetryJobID ||
		record.Code != MediaDiagnosticSequenceIntegrityRejected || record.RejectedAt.IsZero() {
		return SequencePreviewJobProjection{}, ErrSequencePreviewInvalid
	}
	return previews.repository.RejectSequencePreviewArtifact(ctx, record)
}

func (previews *SequencePreviews) ensureExplicitProxy(
	ctx context.Context,
	snapshot SequencePreviewPreparationSnapshot,
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
		AssetID: clip.AssetID, Kind: domain.MediaJobProxy, Profile: SourceProxyProfile,
		ProxySelection: &selection,
	}
	canonical, digest, err := CanonicalInitialMediaJobParameters(parameters)
	if err != nil {
		return domain.WorkJobID{}, err
	}
	jobID, err := previews.newWorkJobID(ctx, at)
	if err != nil {
		return domain.WorkJobID{}, err
	}
	return previews.repository.EnsureExplicitSourceProxyJob(ctx, EnsureExplicitSourceProxyJobRecord{
		JobID: jobID, ProjectID: snapshot.ProjectID, AssetID: clip.AssetID,
		Fingerprint: asset.AcceptedFingerprint, SourceStreams: []domain.SourceStream{stream}, Parameters: parameters,
		Canonical: canonical, Digest: digest, LogicalKey: "media/v1/" + clip.AssetID.String() +
			"/proxy/" + digest.String(), CreatedAt: at,
	})
}

func (previews *SequencePreviews) newWorkJobID(
	ctx context.Context,
	at time.Time,
) (domain.WorkJobID, error) {
	value, err := previews.identities.NewID(ctx, at)
	if err != nil {
		return domain.WorkJobID{}, err
	}
	return domain.ParseWorkJobID(value)
}

func activeSequencePreviewClips(source []domain.ClipState) []domain.ClipState {
	result := make([]domain.ClipState, 0, len(source))
	for _, clip := range source {
		if clip.Enabled && !clip.Tombstoned {
			result = append(result, clip)
		}
	}
	return result
}

func activeSequencePreviewCaptions(source []domain.CaptionState) []domain.CaptionState {
	result := make([]domain.CaptionState, 0, len(source))
	for _, caption := range source {
		if !caption.Tombstoned {
			result = append(result, caption)
		}
	}
	return result
}

func selectSequencePreviewCandidate(
	candidates []SequencePreviewProxyCandidate,
	streamID domain.SourceStreamID,
) (domain.WorkJobID, bool) {
	for _, candidate := range candidates {
		if renderManifestContainsSourceStream(candidate.Manifest, streamID) {
			return candidate.ProducerJobID, true
		}
	}
	return domain.WorkJobID{}, false
}

func renderManifestContainsSourceStream(
	manifest SourceProxyArtifactManifest,
	streamID domain.SourceStreamID,
) bool {
	return manifest.Video != nil && manifest.Video.Source.ID == streamID ||
		manifest.Audio != nil && manifest.Audio.Source.ID == streamID
}

func NormalizeSequencePreviewJobParameters(
	parameters SequencePreviewJobParameters,
) (SequencePreviewJobParameters, error) {
	parameters.Inputs = append([]SequencePreviewInputRequirement(nil), parameters.Inputs...)
	parameters.Resources = append([]SequencePreviewResourcePin(nil), parameters.Resources...)
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
		return SequencePreviewJobParameters{}, err
	}
	return parameters, nil
}

func (parameters SequencePreviewJobParameters) Validate() error {
	if parameters.ProjectID.IsZero() || parameters.SequenceID.IsZero() ||
		parameters.SequenceRevision.Value() == 0 || parameters.ResolverVersion != SequencePreviewResolverV1 ||
		parameters.CompilerVersion != domain.RenderPlanCompilerV4 ||
		parameters.RendererVersion == "" || len(parameters.RendererVersion) > 1024 ||
		!validPreviewTarget(parameters.RendererTarget) || parameters.OutputProfile != domain.SequencePreviewProfileV1 ||
		len(parameters.Inputs) > MaximumRenderPlanItems || len(parameters.Resources) > 32 ||
		(len(parameters.Inputs) == 0 && len(parameters.Resources) == 0) {
		return ErrSequencePreviewInvalid
	}
	previousClip := ""
	for _, input := range parameters.Inputs {
		if input.ClipID.IsZero() || input.SourceStreamID.IsZero() || input.ProducerJobID.IsZero() ||
			input.ClipID.String() <= previousClip {
			return ErrSequencePreviewInvalid
		}
		previousClip = input.ClipID.String()
	}
	previousResource := ""
	for _, resource := range parameters.Resources {
		key := resource.Kind + "\x00" + resource.ID
		if resource.Kind != "font-bundle" || !validPreviewText(resource.ID, 256) ||
			!validPreviewText(resource.Version, 128) || key <= previousResource {
			return ErrSequencePreviewInvalid
		}
		if _, err := domain.ParseDigest(resource.SHA256.String()); err != nil {
			return ErrSequencePreviewInvalid
		}
		previousResource = key
	}
	return nil
}

func CanonicalSequencePreviewJobParameters(
	parameters SequencePreviewJobParameters,
) ([]byte, domain.Digest, SequencePreviewJobParameters, error) {
	normalized, err := NormalizeSequencePreviewJobParameters(parameters)
	if err != nil {
		return nil, "", SequencePreviewJobParameters{}, err
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/sequence-preview-job-parameters", SequencePreviewJobParametersSchema, normalized,
	)
	if err != nil {
		return nil, "", SequencePreviewJobParameters{}, err
	}
	return canonical, digest, normalized, nil
}

func DecodeSequencePreviewJobParameters(data []byte) (SequencePreviewJobParameters, error) {
	var envelope struct {
		Domain  string                       `json:"domain"`
		Payload SequencePreviewJobParameters `json:"payload"`
		Schema  string                       `json:"schema"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		envelope.Domain != "open-cut/sequence-preview-job-parameters" ||
		envelope.Schema != SequencePreviewJobParametersSchema || envelope.Payload.Validate() != nil {
		return SequencePreviewJobParameters{}, ErrSequencePreviewInvalid
	}
	return envelope.Payload, nil
}

func validPreviewTarget(value string) bool {
	switch value {
	case "mac-arm64", "mac-x64", "win-arm64", "win-x64", "linux-arm64", "linux-x64":
		return true
	default:
		return false
	}
}

func validPreviewText(value string, maximum int) bool {
	return value != "" && len([]byte(value)) <= maximum && strings.TrimSpace(value) == value
}
