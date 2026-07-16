package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

var (
	ErrEditInvalid         = errors.New("edit request is invalid")
	ErrEditConflict        = errors.New("edit precondition conflict")
	ErrEditStaleTurn       = errors.New("edit AgentTurn is stale")
	ErrProposalNotFound    = errors.New("edit proposal not found")
	ErrProposalStale       = errors.New("edit proposal is stale")
	ErrProposalTerminal    = errors.New("edit proposal is terminal")
	ErrTransactionNotFound = errors.New("edit transaction not found")
	ErrEditRequestReused   = errors.New("edit request identity was reused")
	ErrEditEntityNotFound  = errors.New("edit entity not found")
)

const MaximumEditIntentBytes = 4000

type EditReference struct {
	ID    string          `json:"id,omitempty" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
	Local *domain.LocalID `json:"local,omitempty" pattern:"^[a-z][a-z0-9_-]{0,63}$"`
}

type TranscriptCorrectionReferenceInput struct {
	Correction EditReference    `json:"correction"`
	Revision   *domain.Revision `json:"revision,omitempty" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
}

type DerivedCaptionOutputInput struct {
	CaptionAs     domain.LocalID   `json:"captionAs" pattern:"^[a-z][a-z0-9_-]{0,63}$"`
	AlignmentAs   domain.LocalID   `json:"alignmentAs" pattern:"^[a-z][a-z0-9_-]{0,63}$"`
	SourceRange   domain.TimeRange `json:"sourceRange"`
	TimelineRange domain.TimeRange `json:"timelineRange"`
	Text          string           `json:"text" minLength:"1" maxLength:"262144"`
}

type AlignmentTargetInput struct {
	Type             domain.AlignmentTargetType `json:"type" enum:"caption,clip,timeline"`
	Caption          *EditReference             `json:"caption,omitempty"`
	Clip             *EditReference             `json:"clip,omitempty"`
	LocalRange       *domain.TimeRange          `json:"localRange,omitempty"`
	TimelineRange    *domain.TimeRange          `json:"timelineRange,omitempty"`
	SequenceRevision *domain.Revision           `json:"sequenceRevision,omitempty" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
}

type ClipSplitOutputInput struct {
	Clip    EditReference  `json:"clip"`
	LeftAs  domain.LocalID `json:"leftAs" pattern:"^[a-z][a-z0-9_-]{0,63}$"`
	RightAs domain.LocalID `json:"rightAs" pattern:"^[a-z][a-z0-9_-]{0,63}$"`
}

type RoughCutLaneBindingInput struct {
	TrackID        domain.TrackID        `json:"trackId"`
	SourceStreamID domain.SourceStreamID `json:"sourceStreamId"`
}

type RoughCutDerivationItemInput struct {
	SourceExcerptID domain.NarrativeNodeID    `json:"sourceExcerptId"`
	Video           *RoughCutLaneBindingInput `json:"video,omitempty"`
	Audio           *RoughCutLaneBindingInput `json:"audio,omitempty"`
}

type DerivedRoughCutLaneOutputInput struct {
	ClipAs         domain.LocalID        `json:"clipAs" pattern:"^[a-z][a-z0-9_-]{0,63}$"`
	TrackID        domain.TrackID        `json:"trackId"`
	SourceStreamID domain.SourceStreamID `json:"sourceStreamId"`
}

type DerivedRoughCutOutputInput struct {
	SourceExcerptID domain.NarrativeNodeID          `json:"sourceExcerptId"`
	SourceRange     domain.TimeRange                `json:"sourceRange"`
	TimelineRange   domain.TimeRange                `json:"timelineRange"`
	Video           *DerivedRoughCutLaneOutputInput `json:"video,omitempty"`
	Audio           *DerivedRoughCutLaneOutputInput `json:"audio,omitempty"`
	LinkGroupAs     *domain.LocalID                 `json:"linkGroupAs,omitempty" pattern:"^[a-z][a-z0-9_-]{0,63}$"`
	AlignmentAs     domain.LocalID                  `json:"alignmentAs" pattern:"^[a-z][a-z0-9_-]{0,63}$"`
}

type EditOperationInput struct {
	Type                   EditOperationInputType               `json:"type" enum:"insert-section,update-section,insert-authored-text,update-authored-text,insert-visual-intent,update-visual-intent,insert-note,update-note,insert-source-excerpt,move-narrative-node,remove-narrative-node,add-transcript-correction,update-transcript-correction,remove-transcript-correction,derive-captions,derive-rough-cut,add-caption,update-caption,remove-caption,bind-alignment,remap-alignment,mark-alignment-stale,unbind-alignment,add-clip,move-clip,trim-clip,split-clip,remove-clip,link-clips,unlink-clips"`
	CreateAs               *domain.LocalID                      `json:"createAs,omitempty" pattern:"^[a-z][a-z0-9_-]{0,63}$"`
	NodeID                 *domain.NarrativeNodeID              `json:"nodeId,omitempty"`
	ParentID               *domain.NarrativeNodeID              `json:"parentId,omitempty"`
	After                  *EditReference                       `json:"after,omitempty"`
	Title                  *string                              `json:"title,omitempty" maxLength:"262144"`
	Text                   *string                              `json:"text,omitempty" maxLength:"262144"`
	Description            *string                              `json:"description,omitempty" maxLength:"262144"`
	AuthoredTextPurpose    *domain.AuthoredTextPurpose          `json:"authoredTextPurpose,omitempty" enum:"spoken,on-screen"`
	VisualIntentPurpose    *domain.VisualIntentPurpose          `json:"visualIntentPurpose,omitempty" enum:"b-roll,composition,replacement"`
	CaptionID              *domain.CaptionID                    `json:"captionId,omitempty"`
	TrackID                *domain.TrackID                      `json:"trackId,omitempty"`
	Range                  *domain.TimeRange                    `json:"range,omitempty"`
	Language               *domain.CaptionLanguage              `json:"language,omitempty" maxLength:"64"`
	AlignmentID            *domain.AlignmentID                  `json:"alignmentId,omitempty"`
	NarrativeNode          *EditReference                       `json:"narrativeNode,omitempty"`
	AlignmentTargets       []AlignmentTargetInput               `json:"alignmentTargets,omitempty" minItems:"1" maxItems:"64"`
	AssetID                *domain.AssetID                      `json:"assetId,omitempty"`
	SourceStreamID         *domain.SourceStreamID               `json:"sourceStreamId,omitempty"`
	SourceRange            *domain.TimeRange                    `json:"sourceRange,omitempty"`
	TimelineRange          *domain.TimeRange                    `json:"timelineRange,omitempty"`
	Enabled                *bool                                `json:"enabled,omitempty"`
	CreateLinkGroupAs      *domain.LocalID                      `json:"createLinkGroupAs,omitempty" pattern:"^[a-z][a-z0-9_-]{0,63}$"`
	LeftLinkGroupAs        *domain.LocalID                      `json:"leftLinkGroupAs,omitempty" pattern:"^[a-z][a-z0-9_-]{0,63}$"`
	RightLinkGroupAs       *domain.LocalID                      `json:"rightLinkGroupAs,omitempty" pattern:"^[a-z][a-z0-9_-]{0,63}$"`
	LinkGroup              *EditReference                       `json:"linkGroup,omitempty"`
	Clip                   *EditReference                       `json:"clip,omitempty"`
	Clips                  []EditReference                      `json:"clips,omitempty" minItems:"2" maxItems:"64"`
	Scope                  *domain.ClipMutationScope            `json:"scope,omitempty" enum:"linked,single"`
	TimelineStart          *domain.RationalTime                 `json:"timelineStart,omitempty"`
	SplitAt                *domain.RationalTime                 `json:"splitAt,omitempty"`
	SplitOutputs           []ClipSplitOutputInput               `json:"splitOutputs,omitempty" minItems:"1" maxItems:"64"`
	TranscriptCorrectionID *domain.TranscriptCorrectionID       `json:"transcriptCorrectionId,omitempty"`
	TranscriptArtifactID   *domain.ArtifactID                   `json:"transcriptArtifactId,omitempty"`
	TranscriptSegmentIDs   []domain.TranscriptSegmentID         `json:"transcriptSegmentIds,omitempty" maxItems:"256"`
	CorrectionRevisions    []TranscriptCorrectionReferenceInput `json:"correctionRevisions,omitempty" maxItems:"256"`
	AcceptedFingerprint    *domain.Digest                       `json:"acceptedFingerprint,omitempty" format:"sha256-digest"`
	CaptionPolicy          *domain.CaptionDerivationPolicy      `json:"captionPolicy,omitempty"`
	DerivedCaptions        []DerivedCaptionOutputInput          `json:"derivedCaptions,omitempty" minItems:"1" maxItems:"128"`
	RoughCutPolicy         *domain.RoughCutDerivationPolicy     `json:"roughCutPolicy,omitempty"`
	RoughCutTimelineStart  *domain.RationalTime                 `json:"roughCutTimelineStart,omitempty"`
	RoughCutLocalPrefix    *domain.LocalID                      `json:"roughCutLocalPrefix,omitempty" pattern:"^[a-z][a-z0-9_-]{0,39}$"`
	RoughCutItems          []RoughCutDerivationItemInput        `json:"roughCutItems,omitempty" minItems:"1" maxItems:"128"`
	DerivedRoughCut        []DerivedRoughCutOutputInput         `json:"derivedRoughCut,omitempty" minItems:"1" maxItems:"128"`
	RoughCutOutputDigest   *domain.Digest                       `json:"roughCutOutputDigest,omitempty" format:"sha256-digest"`
}

type EditOperationInputType = domain.EditOperationType

type EditProposeInput struct {
	RequestID           domain.RequestID            `json:"requestId" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	Intent              string                      `json:"intent" minLength:"1" maxLength:"4000"`
	BaseProjectRevision domain.Revision             `json:"baseProjectRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Preconditions       []domain.EntityPrecondition `json:"preconditions" maxItems:"2048" nullable:"false"`
	Operations          []EditOperationInput        `json:"operations" minItems:"1" maxItems:"512" nullable:"false"`
}

type EditApplyInput struct {
	RequestID      domain.RequestID `json:"requestId" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	ProposalDigest domain.Digest    `json:"proposalDigest" format:"sha256-digest" pattern:"^sha256:[0-9a-f]{64}$"`
}

type EditUndoInput struct {
	RequestID domain.RequestID `json:"requestId" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	Intent    string           `json:"intent,omitempty" maxLength:"4000"`
}

type EditProposalResult struct {
	Proposal       domain.EditProposal `json:"proposal"`
	ActivityCursor domain.Cursor       `json:"activityCursor"`
	Replayed       bool                `json:"replayed"`
}

type EditCommitResult struct {
	Proposal       domain.EditProposal    `json:"proposal"`
	Transaction    domain.EditTransaction `json:"transaction"`
	ActivityCursor domain.Cursor          `json:"activityCursor"`
	Replayed       bool                   `json:"replayed"`
}

type ProposeEditRecord struct {
	ProjectID       domain.ProjectID
	SequenceID      domain.SequenceID
	RunID           domain.RunID
	TurnID          domain.TurnID
	Actor           domain.ActorRef
	RequestID       domain.RequestID
	InputDigest     domain.Digest
	InputCanonical  []byte
	ProposalID      domain.ProposalID
	ActivityEventID domain.ActivityEventID
	Allocation      []domain.LocalAllocation
	Input           EditProposeInput
	CreatedAt       time.Time
}

type ApplyEditRecord struct {
	ProjectID       domain.ProjectID
	SequenceID      domain.SequenceID
	RunID           domain.RunID
	TurnID          domain.TurnID
	Actor           domain.ActorRef
	ProposalID      domain.ProposalID
	RequestID       domain.RequestID
	InputDigest     domain.Digest
	InputCanonical  []byte
	ApplicationID   domain.ProposalApplicationID
	TransactionID   domain.TransactionID
	ActivityEventID domain.ActivityEventID
	Input           EditApplyInput
	OccurredAt      time.Time
}

type UndoEditRecord struct {
	ProjectID           domain.ProjectID
	SequenceID          domain.SequenceID
	RunID               domain.RunID
	TurnID              domain.TurnID
	Actor               domain.ActorRef
	TargetTransactionID domain.TransactionID
	RequestID           domain.RequestID
	InputDigest         domain.Digest
	InputCanonical      []byte
	ProposalID          domain.ProposalID
	ApplicationID       domain.ProposalApplicationID
	TransactionID       domain.TransactionID
	ActivityEventID     domain.ActivityEventID
	Input               EditUndoInput
	OccurredAt          time.Time
}

type CommitCreatorEditRecord struct {
	ProjectID       domain.ProjectID
	SequenceID      domain.SequenceID
	Actor           domain.ActorRef
	RequestID       domain.RequestID
	InputDigest     domain.Digest
	InputCanonical  []byte
	ProposalID      domain.ProposalID
	ApplicationID   domain.ProposalApplicationID
	TransactionID   domain.TransactionID
	ActivityEventID domain.ActivityEventID
	Allocation      []domain.LocalAllocation
	Input           EditProposeInput
	OccurredAt      time.Time
}

type UndoCreatorEditRecord struct {
	ProjectID           domain.ProjectID
	SequenceID          domain.SequenceID
	Actor               domain.ActorRef
	TargetTransactionID domain.TransactionID
	RequestID           domain.RequestID
	InputDigest         domain.Digest
	InputCanonical      []byte
	ProposalID          domain.ProposalID
	ApplicationID       domain.ProposalApplicationID
	TransactionID       domain.TransactionID
	ActivityEventID     domain.ActivityEventID
	Input               EditUndoInput
	OccurredAt          time.Time
}

type EditRepository interface {
	ProposeEdit(context.Context, ProposeEditRecord) (EditProposalResult, error)
	ApplyEdit(context.Context, ApplyEditRecord) (EditCommitResult, error)
	UndoEdit(context.Context, UndoEditRecord) (EditCommitResult, error)
	CommitCreatorEdit(context.Context, CommitCreatorEditRecord) (EditCommitResult, error)
	UndoCreatorEdit(context.Context, UndoCreatorEditRecord) (EditCommitResult, error)
}

type Edits struct {
	repository EditRepository
	identities IdentityGenerator
	clock      Clock
}

func NewEdits(repository EditRepository, identities IdentityGenerator, clock Clock) (*Edits, error) {
	if repository == nil || identities == nil || clock == nil {
		return nil, fmt.Errorf("edit application dependencies are required")
	}
	return &Edits{repository: repository, identities: identities, clock: clock}, nil
}

func (edits *Edits) Propose(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	runID domain.RunID,
	turnID domain.TurnID,
	input EditProposeInput,
) (EditProposalResult, error) {
	authority, err := editAuthority(ctx)
	if err != nil {
		return EditProposalResult{}, err
	}
	if err := validateEditProposeInput(input); err != nil {
		return EditProposalResult{}, err
	}
	now := edits.clock.Now().UTC()
	proposalID, eventID, allocation, err := edits.allocateProposal(ctx, now, input.Operations)
	if err != nil {
		return EditProposalResult{}, err
	}
	canonical, digest, err := editRequestDigest(
		"edit propose", authority.Actor, projectID, sequenceID, runID, turnID, input,
	)
	if err != nil {
		return EditProposalResult{}, err
	}
	return edits.repository.ProposeEdit(ctx, ProposeEditRecord{
		ProjectID: projectID, SequenceID: sequenceID, RunID: runID, TurnID: turnID,
		Actor: authority.Actor, RequestID: input.RequestID, InputDigest: digest,
		InputCanonical: canonical, ProposalID: proposalID, ActivityEventID: eventID,
		Allocation: allocation, Input: input, CreatedAt: now,
	})
}

func (edits *Edits) Apply(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	runID domain.RunID,
	turnID domain.TurnID,
	proposalID domain.ProposalID,
	input EditApplyInput,
) (EditCommitResult, error) {
	authority, err := editAuthority(ctx)
	if err != nil {
		return EditCommitResult{}, err
	}
	if _, err := domain.ParseRequestID(input.RequestID.String()); err != nil {
		return EditCommitResult{}, ErrEditInvalid
	}
	if proposalID.IsZero() {
		return EditCommitResult{}, ErrEditInvalid
	}
	if _, err := domain.ParseDigest(input.ProposalDigest.String()); err != nil {
		return EditCommitResult{}, ErrEditInvalid
	}
	now := edits.clock.Now().UTC()
	applicationID, transactionID, eventID, err := edits.allocateApply(ctx, now)
	if err != nil {
		return EditCommitResult{}, err
	}
	canonical, digest, err := editRequestDigest(
		"edit apply", authority.Actor, projectID, sequenceID, runID, turnID,
		struct {
			ProposalID domain.ProposalID `json:"proposalId"`
			Input      EditApplyInput    `json:"input"`
		}{ProposalID: proposalID, Input: input},
	)
	if err != nil {
		return EditCommitResult{}, err
	}
	return edits.repository.ApplyEdit(ctx, ApplyEditRecord{
		ProjectID: projectID, SequenceID: sequenceID, RunID: runID, TurnID: turnID,
		Actor: authority.Actor, ProposalID: proposalID, RequestID: input.RequestID,
		InputDigest: digest, InputCanonical: canonical, ApplicationID: applicationID,
		TransactionID: transactionID, ActivityEventID: eventID, Input: input, OccurredAt: now,
	})
}

func (edits *Edits) Undo(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	runID domain.RunID,
	turnID domain.TurnID,
	target domain.TransactionID,
	input EditUndoInput,
) (EditCommitResult, error) {
	authority, err := editAuthority(ctx)
	if err != nil {
		return EditCommitResult{}, err
	}
	if target.IsZero() || validateEditIntent(input.Intent, true) != nil {
		return EditCommitResult{}, ErrEditInvalid
	}
	if _, err := domain.ParseRequestID(input.RequestID.String()); err != nil {
		return EditCommitResult{}, ErrEditInvalid
	}
	now := edits.clock.Now().UTC()
	proposalID, applicationID, transactionID, eventID, err := edits.allocateUndo(ctx, now)
	if err != nil {
		return EditCommitResult{}, err
	}
	canonical, digest, err := editRequestDigest(
		"edit undo", authority.Actor, projectID, sequenceID, runID, turnID,
		struct {
			TransactionID domain.TransactionID `json:"transactionId"`
			Input         EditUndoInput        `json:"input"`
		}{TransactionID: target, Input: input},
	)
	if err != nil {
		return EditCommitResult{}, err
	}
	return edits.repository.UndoEdit(ctx, UndoEditRecord{
		ProjectID: projectID, SequenceID: sequenceID, RunID: runID, TurnID: turnID,
		Actor: authority.Actor, TargetTransactionID: target, RequestID: input.RequestID,
		InputDigest: digest, InputCanonical: canonical, ProposalID: proposalID,
		ApplicationID: applicationID, TransactionID: transactionID,
		ActivityEventID: eventID, Input: input, OccurredAt: now,
	})
}

func (edits *Edits) CommitForCreator(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	input EditProposeInput,
) (EditCommitResult, error) {
	authority, err := creatorAuthority(ctx)
	if err != nil {
		return EditCommitResult{}, err
	}
	if projectID.IsZero() || sequenceID.IsZero() || validateEditProposeInput(input) != nil {
		return EditCommitResult{}, ErrEditInvalid
	}
	now := edits.clock.Now().UTC()
	proposalID, _, allocation, err := edits.allocateProposal(ctx, now, input.Operations)
	if err != nil {
		return EditCommitResult{}, err
	}
	applicationID, transactionID, eventID, err := edits.allocateApply(ctx, now)
	if err != nil {
		return EditCommitResult{}, err
	}
	canonical, digest, err := creatorEditRequestDigest(
		"commit", authority.Actor, projectID, sequenceID, input,
	)
	if err != nil {
		return EditCommitResult{}, err
	}
	return edits.repository.CommitCreatorEdit(ctx, CommitCreatorEditRecord{
		ProjectID: projectID, SequenceID: sequenceID, Actor: authority.Actor,
		RequestID: input.RequestID, InputDigest: digest, InputCanonical: canonical,
		ProposalID: proposalID, ApplicationID: applicationID, TransactionID: transactionID,
		ActivityEventID: eventID, Allocation: allocation, Input: input, OccurredAt: now,
	})
}

func (edits *Edits) UndoForCreator(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	target domain.TransactionID,
	input EditUndoInput,
) (EditCommitResult, error) {
	authority, err := creatorAuthority(ctx)
	if err != nil {
		return EditCommitResult{}, err
	}
	if projectID.IsZero() || sequenceID.IsZero() || target.IsZero() || validateEditIntent(input.Intent, true) != nil {
		return EditCommitResult{}, ErrEditInvalid
	}
	if _, err := domain.ParseRequestID(input.RequestID.String()); err != nil {
		return EditCommitResult{}, ErrEditInvalid
	}
	now := edits.clock.Now().UTC()
	proposalID, applicationID, transactionID, eventID, err := edits.allocateUndo(ctx, now)
	if err != nil {
		return EditCommitResult{}, err
	}
	canonical, digest, err := creatorEditRequestDigest(
		"undo", authority.Actor, projectID, sequenceID, struct {
			TransactionID domain.TransactionID `json:"transactionId"`
			Input         EditUndoInput        `json:"input"`
		}{TransactionID: target, Input: input},
	)
	if err != nil {
		return EditCommitResult{}, err
	}
	return edits.repository.UndoCreatorEdit(ctx, UndoCreatorEditRecord{
		ProjectID: projectID, SequenceID: sequenceID, Actor: authority.Actor,
		TargetTransactionID: target, RequestID: input.RequestID, InputDigest: digest,
		InputCanonical: canonical, ProposalID: proposalID, ApplicationID: applicationID,
		TransactionID: transactionID, ActivityEventID: eventID, Input: input, OccurredAt: now,
	})
}

func editAuthority(ctx context.Context) (Authority, error) {
	authority, err := AuthorityFromContext(ctx)
	if err != nil {
		return Authority{}, err
	}
	if authority.Surface != AuthorityProductCLI || authority.Actor.Kind != domain.ActorAgent {
		return Authority{}, ErrAuthorityScopeDenied
	}
	return authority, nil
}

func editRequestDigest(
	commandName string,
	actor domain.ActorRef,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	runID domain.RunID,
	turnID domain.TurnID,
	input any,
) ([]byte, domain.Digest, error) {
	return domain.CanonicalDigest("open-cut/command-request/"+commandName, domain.EditRequestSchema, struct {
		Actor      domain.ActorRef   `json:"actor"`
		ProjectID  domain.ProjectID  `json:"projectId"`
		RunID      domain.RunID      `json:"runId"`
		SequenceID domain.SequenceID `json:"sequenceId"`
		TurnID     domain.TurnID     `json:"turnId"`
		Input      any               `json:"input"`
	}{Actor: actor, ProjectID: projectID, SequenceID: sequenceID, RunID: runID, TurnID: turnID, Input: input})
}

func creatorEditRequestDigest(
	operation string,
	actor domain.ActorRef,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	input any,
) ([]byte, domain.Digest, error) {
	return domain.CanonicalDigest("open-cut/creator-edit-request/"+operation, domain.EditRequestSchema, struct {
		Actor      domain.ActorRef   `json:"actor"`
		ProjectID  domain.ProjectID  `json:"projectId"`
		SequenceID domain.SequenceID `json:"sequenceId"`
		Input      any               `json:"input"`
	}{Actor: actor, ProjectID: projectID, SequenceID: sequenceID, Input: input})
}
