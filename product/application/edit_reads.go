package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

var ErrInvalidEditCursor = errors.New("invalid edit read cursor")

type CaptionDerivationPreviewInput struct {
	SourceExcerptID domain.NarrativeNodeID `json:"sourceExcerptId"`
	ClipID          domain.ClipID          `json:"clipId"`
	TrackID         domain.TrackID         `json:"trackId"`
	LocalPrefix     string                 `json:"localPrefix" pattern:"^[a-z][a-z0-9_-]{0,39}$" default:"derived"`
}

type CreatorCaptionDerivationPreviewInput struct {
	SourceExcerptID       domain.NarrativeNodeID `json:"sourceExcerptId"`
	SourceExcerptRevision domain.Revision        `json:"sourceExcerptRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	ClipID                domain.ClipID          `json:"clipId"`
	ClipRevision          domain.Revision        `json:"clipRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	TrackID               domain.TrackID         `json:"trackId"`
	TrackRevision         domain.Revision        `json:"trackRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	LocalPrefix           domain.LocalID         `json:"localPrefix" pattern:"^[a-z][a-z0-9_-]{0,39}$"`
}

type CaptionDerivationPreviewQuery struct {
	ProjectID  domain.ProjectID
	SequenceID domain.SequenceID
	Input      CaptionDerivationPreviewInput
}

type CaptionDerivationPreview struct {
	BaseProjectRevision domain.Revision             `json:"baseProjectRevision"`
	Preconditions       []domain.EntityPrecondition `json:"preconditions" maxItems:"260" nullable:"false"`
	Operation           EditOperationInput          `json:"operation"`
	Language            domain.CaptionLanguage      `json:"language" maxLength:"64"`
	ActivityCursor      domain.Cursor               `json:"activityCursor"`
}

type RoughCutDerivationPreviewLaneInput struct {
	TrackID        domain.TrackID        `json:"trackId"`
	TrackRevision  domain.Revision       `json:"trackRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	SourceStreamID domain.SourceStreamID `json:"sourceStreamId"`
}

type RoughCutDerivationPreviewItemInput struct {
	SourceExcerptID       domain.NarrativeNodeID              `json:"sourceExcerptId"`
	SourceExcerptRevision domain.Revision                     `json:"sourceExcerptRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Video                 *RoughCutDerivationPreviewLaneInput `json:"video,omitempty"`
	Audio                 *RoughCutDerivationPreviewLaneInput `json:"audio,omitempty"`
}

type RoughCutDerivationPreviewInput struct {
	TimelineStart domain.RationalTime                  `json:"timelineStart"`
	LocalPrefix   domain.LocalID                       `json:"localPrefix" pattern:"^[a-z][a-z0-9_-]{0,39}$"`
	Items         []RoughCutDerivationPreviewItemInput `json:"items" minItems:"1" maxItems:"128" nullable:"false"`
}

type RoughCutDerivationPreviewQuery struct {
	ProjectID  domain.ProjectID
	SequenceID domain.SequenceID
	Input      RoughCutDerivationPreviewInput
}

type RoughCutDerivationPreview struct {
	BaseProjectRevision domain.Revision             `json:"baseProjectRevision"`
	Preconditions       []domain.EntityPrecondition `json:"preconditions" maxItems:"2048" nullable:"false"`
	Operation           EditOperationInput          `json:"operation"`
	OutputDigest        domain.Digest               `json:"outputDigest" format:"sha256-digest"`
	ActivityCursor      domain.Cursor               `json:"activityCursor"`
}

type NarrativeSubtreeQuery struct {
	ProjectID             domain.ProjectID
	DocumentID            domain.NarrativeDocumentID
	ParentID              domain.NarrativeNodeID
	AfterID               string
	AfterDocumentRevision domain.Revision
	AfterParentRevision   domain.Revision
	Limit                 int
}

type NarrativeSubtreePage struct {
	DocumentID       domain.NarrativeDocumentID  `json:"documentId"`
	DocumentRevision domain.Revision             `json:"documentRevision"`
	Parent           NarrativeSectionSummary     `json:"parent"`
	Nodes            []domain.NarrativeNodeState `json:"nodes" maxItems:"200" nullable:"false"`
	NextAfter        string                      `json:"nextAfter,omitempty"`
	ActivityCursor   domain.Cursor               `json:"activityCursor"`
}

type NarrativeSectionSummary struct {
	ID       domain.NarrativeNodeID `json:"id"`
	Revision domain.Revision        `json:"revision"`
	Title    string                 `json:"title"`
	Language domain.CaptionLanguage `json:"language" maxLength:"64"`
}

type NarrativeSubtreeResult struct {
	DocumentID       domain.NarrativeDocumentID
	DocumentRevision domain.Revision
	Parent           NarrativeSectionSummary
	Nodes            []domain.NarrativeNodeState
	HasMore          bool
	ActivityCursor   domain.Cursor
}

type SequenceWindowQuery struct {
	ProjectID  domain.ProjectID
	SequenceID domain.SequenceID
	TrackID    *domain.TrackID
	StartKey   string
	EndKey     string
	AfterKey   string
	AfterKind  string
	AfterID    string
	Limit      int
}

type SequenceWindowResult struct {
	SequenceID       domain.SequenceID
	SequenceRevision domain.Revision
	Range            domain.TimeRange
	Captions         []domain.CaptionState
	Clips            []domain.ClipState
	LinkGroups       []domain.LinkGroupState
	Alignments       []domain.AlignmentState
	HasMore          bool
	NextKey          string
	NextKind         string
	NextID           string
	ActivityCursor   domain.Cursor
}

type SequenceWindowPage struct {
	SequenceID       domain.SequenceID       `json:"sequenceId"`
	SequenceRevision domain.Revision         `json:"sequenceRevision"`
	Range            domain.TimeRange        `json:"range"`
	Captions         []domain.CaptionState   `json:"captions" maxItems:"512" nullable:"false"`
	Clips            []domain.ClipState      `json:"clips" maxItems:"512" nullable:"false"`
	LinkGroups       []domain.LinkGroupState `json:"linkGroups" maxItems:"512" nullable:"false"`
	Alignments       []domain.AlignmentState `json:"alignments" maxItems:"2048" nullable:"false"`
	NextAfter        string                  `json:"nextAfter,omitempty"`
	ActivityCursor   domain.Cursor           `json:"activityCursor"`
}

type EditEntityDetail struct {
	Kind                        domain.EditEntityKind              `json:"kind" enum:"narrative-node,transcript-correction,caption,alignment,clip,link-group"`
	AuthoredText                *domain.AuthoredTextState          `json:"authoredText,omitempty"`
	SourceExcerpt               *domain.SourceExcerptState         `json:"sourceExcerpt,omitempty"`
	Section                     *domain.NarrativeSectionState      `json:"section,omitempty"`
	VisualIntent                *domain.VisualIntentState          `json:"visualIntent,omitempty"`
	Note                        *domain.NoteState                  `json:"note,omitempty"`
	TranscriptCorrection        *domain.TranscriptCorrectionState  `json:"transcriptCorrection,omitempty"`
	SourceExcerptEvidenceStatus domain.SourceExcerptEvidenceStatus `json:"sourceExcerptEvidenceStatus,omitempty" enum:"exact,stale"`
	Caption                     *domain.CaptionState               `json:"caption,omitempty"`
	Alignment                   *domain.AlignmentState             `json:"alignment,omitempty"`
	Clip                        *domain.ClipState                  `json:"clip,omitempty"`
	LinkGroup                   *domain.LinkGroupState             `json:"linkGroup,omitempty"`
	ActivityCursor              domain.Cursor                      `json:"activityCursor"`
}

type TransactionHistoryQuery struct {
	ProjectID     domain.ProjectID
	AfterRevision domain.Revision
	Limit         int
}

type TransactionHistoryResult struct {
	Transactions   []domain.EditTransaction
	HasMore        bool
	ActivityCursor domain.Cursor
}

type TransactionHistoryPage struct {
	Transactions   []domain.EditTransaction `json:"transactions" maxItems:"100" nullable:"false"`
	NextAfter      domain.Revision          `json:"nextAfter,omitempty"`
	ActivityCursor domain.Cursor            `json:"activityCursor"`
}

type CreatorTransactionHistoryQuery struct {
	ProjectID      domain.ProjectID
	BeforeRevision domain.Revision
	Limit          int
}

type CreatorTransactionHistoryItem struct {
	ID                       domain.TransactionID          `json:"id"`
	Intent                   string                        `json:"intent" maxLength:"262144"`
	Actor                    domain.CreativeActor          `json:"actor" enum:"creator,agent"`
	CommittedProjectRevision domain.Revision               `json:"committedProjectRevision"`
	Changes                  []domain.EntityRevisionChange `json:"changes" maxItems:"2048" nullable:"false"`
	UndoesTransactionID      *domain.TransactionID         `json:"undoesTransactionId,omitempty"`
	CommittedAt              time.Time                     `json:"committedAt"`
}

type CreatorTransactionHistoryResult struct {
	Transactions   []CreatorTransactionHistoryItem
	HasMore        bool
	ActivityCursor domain.Cursor
}

type CreatorTransactionHistoryPage struct {
	Transactions   []CreatorTransactionHistoryItem `json:"transactions" maxItems:"50" nullable:"false"`
	NextBefore     domain.Revision                 `json:"nextBefore,omitempty"`
	ActivityCursor domain.Cursor                   `json:"activityCursor"`
}

type EditReadRepository interface {
	ReadCaptionDerivationPreview(context.Context, CaptionDerivationPreviewQuery) (CaptionDerivationPreview, error)
	ReadRoughCutDerivationPreview(context.Context, RoughCutDerivationPreviewQuery) (RoughCutDerivationPreview, error)
	ReadCreatorTimelineGesturePreview(context.Context, CreatorTimelineGesturePreviewQuery) (CreatorTimelineGesturePreviewResult, error)
	ReadCreatorCaptionGesturePreview(context.Context, CreatorCaptionGesturePreviewQuery) (CreatorCaptionGesturePreview, error)
	ReadCreatorClipPlacementPreview(context.Context, CreatorClipPlacementPreviewQuery) (CreatorClipPlacementPreview, error)
	ReadNarrativeSubtree(context.Context, NarrativeSubtreeQuery) (NarrativeSubtreeResult, error)
	ReadSequenceWindow(context.Context, SequenceWindowQuery) (SequenceWindowResult, error)
	ReadEditEntity(context.Context, domain.ProjectID, domain.EditEntityKind, string) (EditEntityDetail, error)
	ReadEditProposal(context.Context, domain.ProjectID, domain.ProposalID) (domain.EditProposal, domain.Cursor, error)
	ReadTransactionHistory(context.Context, TransactionHistoryQuery) (TransactionHistoryResult, error)
	ReadCreatorTransactionHistory(context.Context, CreatorTransactionHistoryQuery) (CreatorTransactionHistoryResult, error)
}

func (reads *EditReads) RoughCutDerivation(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	input RoughCutDerivationPreviewInput,
) (RoughCutDerivationPreview, error) {
	authority, err := AuthorityFromContext(ctx)
	if err != nil {
		return RoughCutDerivationPreview{}, err
	}
	if authority.Surface != AuthorityProductCLI || authority.Actor.Kind != domain.ActorAgent {
		return RoughCutDerivationPreview{}, ErrAuthorityScopeDenied
	}
	return reads.roughCutDerivation(ctx, projectID, sequenceID, input)
}

func (reads *EditReads) RoughCutDerivationForCreator(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	input RoughCutDerivationPreviewInput,
) (RoughCutDerivationPreview, error) {
	if _, err := creatorAuthority(ctx); err != nil {
		return RoughCutDerivationPreview{}, err
	}
	return reads.roughCutDerivation(ctx, projectID, sequenceID, input)
}

func (reads *EditReads) TimelineGestureForCreator(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	input CreatorTimelineGesturePreviewInput,
) (CreatorTimelineGesturePreviewResult, error) {
	authority, err := creatorAuthority(ctx)
	if err != nil {
		return CreatorTimelineGesturePreviewResult{}, err
	}
	if projectID.IsZero() || sequenceID.IsZero() || !validCreatorTimelineGesture(input) {
		return CreatorTimelineGesturePreviewResult{}, ErrEditInvalid
	}
	return reads.repository.ReadCreatorTimelineGesturePreview(ctx, CreatorTimelineGesturePreviewQuery{
		ProjectID: projectID, SequenceID: sequenceID, Actor: authority.Actor, Input: input,
	})
}

func validCreatorTimelineGesture(input CreatorTimelineGesturePreviewInput) bool {
	if input.ClipID.IsZero() || input.ClipRevision.Value() < 1 ||
		(input.Scope != domain.ClipScopeLinked && input.Scope != domain.ClipScopeSingle) ||
		(input.AlignmentHandling != CreatorTimelinePreserveAlignment &&
			input.AlignmentHandling != CreatorTimelineStaleAlignment &&
			input.AlignmentHandling != CreatorTimelineUnbindAlignment) {
		return false
	}
	validTime := func(value *domain.RationalTime) bool {
		return value != nil && value.Validate() == nil && !value.IsNegative()
	}
	validRange := func(value *domain.TimeRange) bool {
		if value == nil || value.Start.Validate() != nil || value.Start.IsNegative() ||
			value.Duration.Validate() != nil || !value.Duration.IsPositive() {
			return false
		}
		_, err := value.End()
		return err == nil
	}
	switch input.Kind {
	case CreatorTimelineMove:
		return input.TrackID != nil && !input.TrackID.IsZero() && input.TrackRevision != nil &&
			input.TrackRevision.Value() > 0 && validTime(input.TimelineStart) && input.SourceRange == nil &&
			input.TimelineRange == nil && input.SplitAt == nil && input.LocalPrefix == nil
	case CreatorTimelineTrim:
		return input.TrackID == nil && input.TrackRevision == nil && input.TimelineStart == nil &&
			validRange(input.SourceRange) && validRange(input.TimelineRange) && input.SplitAt == nil &&
			input.LocalPrefix == nil
	case CreatorTimelineSplit:
		return input.TrackID == nil && input.TrackRevision == nil && input.TimelineStart == nil &&
			input.SourceRange == nil && input.TimelineRange == nil && validTime(input.SplitAt) &&
			input.LocalPrefix != nil && len(input.LocalPrefix.String()) <= 40
	case CreatorTimelineRemove:
		return input.AlignmentHandling != CreatorTimelinePreserveAlignment && input.TrackID == nil &&
			input.TrackRevision == nil && input.TimelineStart == nil && input.SourceRange == nil &&
			input.TimelineRange == nil && input.SplitAt == nil && input.LocalPrefix == nil
	default:
		return false
	}
}

func (reads *EditReads) roughCutDerivation(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	input RoughCutDerivationPreviewInput,
) (RoughCutDerivationPreview, error) {
	if projectID.IsZero() || sequenceID.IsZero() || input.TimelineStart.Validate() != nil ||
		input.TimelineStart.IsNegative() || len(input.Items) == 0 || len(input.Items) > 128 ||
		len(input.LocalPrefix.String()) > 40 {
		return RoughCutDerivationPreview{}, ErrEditInvalid
	}
	if _, err := domain.ParseLocalID(input.LocalPrefix.String()); err != nil {
		return RoughCutDerivationPreview{}, ErrEditInvalid
	}
	for _, item := range input.Items {
		if item.SourceExcerptID.IsZero() || item.SourceExcerptRevision.Value() < 1 ||
			(item.Video == nil && item.Audio == nil) || !validPreviewRoughCutLane(item.Video) ||
			!validPreviewRoughCutLane(item.Audio) {
			return RoughCutDerivationPreview{}, ErrEditInvalid
		}
	}
	return reads.repository.ReadRoughCutDerivationPreview(ctx, RoughCutDerivationPreviewQuery{
		ProjectID: projectID, SequenceID: sequenceID, Input: input,
	})
}

func validPreviewRoughCutLane(value *RoughCutDerivationPreviewLaneInput) bool {
	return value == nil || (!value.TrackID.IsZero() && value.TrackRevision.Value() >= 1 && !value.SourceStreamID.IsZero())
}

type EditReads struct {
	repository EditReadRepository
}

func NewEditReads(repository EditReadRepository) (*EditReads, error) {
	if repository == nil {
		return nil, fmt.Errorf("edit read repository is required")
	}
	return &EditReads{repository: repository}, nil
}

func (reads *EditReads) CaptionDerivation(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	input CaptionDerivationPreviewInput,
) (CaptionDerivationPreview, error) {
	authority, err := AuthorityFromContext(ctx)
	if err != nil {
		return CaptionDerivationPreview{}, err
	}
	if authority.Surface != AuthorityProductCLI || authority.Actor.Kind != domain.ActorAgent ||
		projectID.IsZero() || sequenceID.IsZero() || input.SourceExcerptID.IsZero() ||
		input.ClipID.IsZero() || input.TrackID.IsZero() {
		return CaptionDerivationPreview{}, ErrAuthorityScopeDenied
	}
	return reads.captionDerivation(ctx, projectID, sequenceID, input)
}

func (reads *EditReads) CaptionDerivationForCreator(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	input CreatorCaptionDerivationPreviewInput,
) (CaptionDerivationPreview, error) {
	if _, err := creatorAuthority(ctx); err != nil {
		return CaptionDerivationPreview{}, err
	}
	if projectID.IsZero() || sequenceID.IsZero() || input.SourceExcerptID.IsZero() ||
		input.SourceExcerptRevision.Value() < 1 || input.ClipID.IsZero() || input.ClipRevision.Value() < 1 ||
		input.TrackID.IsZero() || input.TrackRevision.Value() < 1 || input.LocalPrefix.String() == "" {
		return CaptionDerivationPreview{}, ErrEditInvalid
	}
	preview, err := reads.captionDerivation(ctx, projectID, sequenceID, CaptionDerivationPreviewInput{
		SourceExcerptID: input.SourceExcerptID, ClipID: input.ClipID,
		TrackID: input.TrackID, LocalPrefix: input.LocalPrefix.String(),
	})
	if err != nil {
		return CaptionDerivationPreview{}, err
	}
	for _, expected := range []domain.EntityPrecondition{
		{Kind: domain.EntityNarrativeNode, ID: input.SourceExcerptID.String(), Revision: input.SourceExcerptRevision},
		{Kind: domain.EntityClip, ID: input.ClipID.String(), Revision: input.ClipRevision},
		{Kind: domain.EntityTrack, ID: input.TrackID.String(), Revision: input.TrackRevision},
	} {
		if !hasExactEntityPrecondition(preview.Preconditions, expected) {
			return CaptionDerivationPreview{}, ErrEditConflict
		}
	}
	return preview, nil
}

func (reads *EditReads) captionDerivation(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	input CaptionDerivationPreviewInput,
) (CaptionDerivationPreview, error) {
	if input.LocalPrefix == "" {
		input.LocalPrefix = "derived"
	}
	if len(input.LocalPrefix) > 40 {
		return CaptionDerivationPreview{}, ErrEditInvalid
	}
	if _, err := domain.ParseLocalID(input.LocalPrefix); err != nil {
		return CaptionDerivationPreview{}, ErrEditInvalid
	}
	return reads.repository.ReadCaptionDerivationPreview(ctx, CaptionDerivationPreviewQuery{
		ProjectID: projectID, SequenceID: sequenceID, Input: input,
	})
}

func hasExactEntityPrecondition(values []domain.EntityPrecondition, expected domain.EntityPrecondition) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func (reads *EditReads) NarrativeSubtree(
	ctx context.Context,
	projectID domain.ProjectID,
	documentID domain.NarrativeDocumentID,
	parentID domain.NarrativeNodeID,
	after string,
	limit uint16,
) (NarrativeSubtreePage, error) {
	if _, err := AuthorityFromContext(ctx); err != nil {
		return NarrativeSubtreePage{}, err
	}
	pageLimit := boundedLimit(limit, 50, 200)
	if pageLimit == 0 {
		return NarrativeSubtreePage{}, ErrEditInvalid
	}
	afterID, afterDocumentRevision, afterParentRevision, err := decodeNarrativeCursor(after, documentID, parentID)
	if err != nil {
		return NarrativeSubtreePage{}, err
	}
	result, err := reads.repository.ReadNarrativeSubtree(ctx, NarrativeSubtreeQuery{
		ProjectID: projectID, DocumentID: documentID, ParentID: parentID,
		AfterID: afterID, AfterDocumentRevision: afterDocumentRevision,
		AfterParentRevision: afterParentRevision, Limit: pageLimit,
	})
	if err != nil {
		return NarrativeSubtreePage{}, err
	}
	page := NarrativeSubtreePage{
		DocumentID: result.DocumentID, DocumentRevision: result.DocumentRevision,
		Parent: result.Parent, Nodes: result.Nodes, ActivityCursor: result.ActivityCursor,
	}
	if result.HasMore && len(result.Nodes) > 0 {
		page.NextAfter = encodeNarrativeCursor(
			documentID, result.DocumentRevision, parentID, result.Parent.Revision,
			result.Nodes[len(result.Nodes)-1].ID().String(),
		)
	}
	return page, nil
}

func (reads *EditReads) SequenceWindow(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	trackID *domain.TrackID,
	rangeValue domain.TimeRange,
	after string,
	limit uint16,
) (SequenceWindowPage, error) {
	if _, err := AuthorityFromContext(ctx); err != nil {
		return SequenceWindowPage{}, err
	}
	if rangeValue.Start.Validate() != nil || rangeValue.Start.IsNegative() ||
		rangeValue.Duration.Validate() != nil || !rangeValue.Duration.IsPositive() {
		return SequenceWindowPage{}, ErrEditInvalid
	}
	pageLimit := boundedLimit(limit, 100, 512)
	if pageLimit == 0 {
		return SequenceWindowPage{}, ErrEditInvalid
	}
	startKey, err := domain.RationalOrderKey(rangeValue.Start)
	if err != nil {
		return SequenceWindowPage{}, ErrEditInvalid
	}
	end, err := rangeValue.End()
	if err != nil {
		return SequenceWindowPage{}, ErrEditInvalid
	}
	endKey, err := domain.RationalOrderKey(end)
	if err != nil {
		return SequenceWindowPage{}, ErrEditInvalid
	}
	afterKey, afterKind, afterID, err := decodeSequenceCursor(after)
	if err != nil {
		return SequenceWindowPage{}, err
	}
	result, err := reads.repository.ReadSequenceWindow(ctx, SequenceWindowQuery{
		ProjectID: projectID, SequenceID: sequenceID, TrackID: trackID,
		StartKey: startKey, EndKey: endKey, AfterKey: afterKey, AfterKind: afterKind,
		AfterID: afterID, Limit: pageLimit,
	})
	if err != nil {
		return SequenceWindowPage{}, err
	}
	page := SequenceWindowPage{
		SequenceID: result.SequenceID, SequenceRevision: result.SequenceRevision,
		Range: rangeValue, Captions: result.Captions, Clips: result.Clips,
		LinkGroups: result.LinkGroups, Alignments: result.Alignments,
		ActivityCursor: result.ActivityCursor,
	}
	if result.HasMore && result.NextKey != "" && result.NextKind != "" && result.NextID != "" {
		page.NextAfter = encodeSequenceCursor(result.NextKey, result.NextKind, result.NextID)
	}
	return page, nil
}

func (reads *EditReads) Entity(
	ctx context.Context,
	projectID domain.ProjectID,
	kind domain.EditEntityKind,
	id string,
) (EditEntityDetail, error) {
	if _, err := AuthorityFromContext(ctx); err != nil {
		return EditEntityDetail{}, err
	}
	if kind != domain.EntityNarrativeNode && kind != domain.EntityTranscriptCorrection &&
		kind != domain.EntityCaption && kind != domain.EntityAlignment &&
		kind != domain.EntityClip && kind != domain.EntityLinkGroup {
		return EditEntityDetail{}, ErrEditInvalid
	}
	return reads.repository.ReadEditEntity(ctx, projectID, kind, id)
}

func (reads *EditReads) Proposal(
	ctx context.Context,
	projectID domain.ProjectID,
	proposalID domain.ProposalID,
) (domain.EditProposal, domain.Cursor, error) {
	if _, err := AuthorityFromContext(ctx); err != nil {
		return domain.EditProposal{}, 0, err
	}
	return reads.repository.ReadEditProposal(ctx, projectID, proposalID)
}

func (reads *EditReads) History(
	ctx context.Context,
	projectID domain.ProjectID,
	after domain.Revision,
	limit uint16,
) (TransactionHistoryPage, error) {
	if _, err := AuthorityFromContext(ctx); err != nil {
		return TransactionHistoryPage{}, err
	}
	pageLimit := boundedLimit(limit, 50, 100)
	if pageLimit == 0 {
		return TransactionHistoryPage{}, ErrEditInvalid
	}
	result, err := reads.repository.ReadTransactionHistory(ctx, TransactionHistoryQuery{
		ProjectID: projectID, AfterRevision: after, Limit: pageLimit,
	})
	if err != nil {
		return TransactionHistoryPage{}, err
	}
	page := TransactionHistoryPage{Transactions: result.Transactions, ActivityCursor: result.ActivityCursor}
	if result.HasMore && len(result.Transactions) > 0 {
		page.NextAfter = result.Transactions[len(result.Transactions)-1].CommittedProjectRevision
	}
	return page, nil
}

func (reads *EditReads) HistoryForCreator(
	ctx context.Context,
	projectID domain.ProjectID,
	before domain.Revision,
	limit uint16,
) (CreatorTransactionHistoryPage, error) {
	if _, err := creatorAuthority(ctx); err != nil {
		return CreatorTransactionHistoryPage{}, err
	}
	pageLimit := boundedLimit(limit, 20, 50)
	if projectID.IsZero() || pageLimit == 0 {
		return CreatorTransactionHistoryPage{}, ErrEditInvalid
	}
	result, err := reads.repository.ReadCreatorTransactionHistory(ctx, CreatorTransactionHistoryQuery{
		ProjectID: projectID, BeforeRevision: before, Limit: pageLimit,
	})
	if err != nil {
		return CreatorTransactionHistoryPage{}, err
	}
	page := CreatorTransactionHistoryPage{
		Transactions: result.Transactions, ActivityCursor: result.ActivityCursor,
	}
	if result.HasMore && len(result.Transactions) > 0 {
		page.NextBefore = result.Transactions[len(result.Transactions)-1].CommittedProjectRevision
	}
	return page, nil
}

func boundedLimit(value uint16, defaultValue, maximum int) int {
	if value == 0 {
		return defaultValue
	}
	if int(value) > maximum {
		return 0
	}
	return int(value)
}

type narrativeCursorPayload struct {
	DocumentID       string `json:"documentId"`
	DocumentRevision uint64 `json:"documentRevision"`
	ParentID         string `json:"parentId"`
	ParentRevision   uint64 `json:"parentRevision"`
	AfterID          string `json:"afterId"`
}

func encodeNarrativeCursor(
	documentID domain.NarrativeDocumentID,
	documentRevision domain.Revision,
	parentID domain.NarrativeNodeID,
	parentRevision domain.Revision,
	afterID string,
) string {
	payload, _ := json.Marshal(narrativeCursorPayload{
		DocumentID: documentID.String(), DocumentRevision: documentRevision.Value(),
		ParentID: parentID.String(), ParentRevision: parentRevision.Value(), AfterID: afterID,
	})
	return "narrative-node.v2." + base64.RawURLEncoding.EncodeToString(payload)
}

func decodeNarrativeCursor(
	value string,
	documentID domain.NarrativeDocumentID,
	parentID domain.NarrativeNodeID,
) (string, domain.Revision, domain.Revision, error) {
	if value == "" {
		return "", 0, 0, nil
	}
	const prefix = "narrative-node.v2."
	if !strings.HasPrefix(value, prefix) {
		return "", 0, 0, ErrInvalidEditCursor
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil {
		return "", 0, 0, ErrInvalidEditCursor
	}
	var payload narrativeCursorPayload
	if err := json.Unmarshal(decoded, &payload); err != nil ||
		payload.DocumentID != documentID.String() || payload.ParentID != parentID.String() {
		return "", 0, 0, ErrInvalidEditCursor
	}
	id, err := domain.ParseNarrativeNodeID(payload.AfterID)
	if err != nil {
		return "", 0, 0, ErrInvalidEditCursor
	}
	documentRevision, err := domain.NewRevision(payload.DocumentRevision)
	if err != nil {
		return "", 0, 0, ErrInvalidEditCursor
	}
	parentRevision, err := domain.NewRevision(payload.ParentRevision)
	if err != nil {
		return "", 0, 0, ErrInvalidEditCursor
	}
	return id.String(), documentRevision, parentRevision, nil
}

func encodeSequenceCursor(key, kind, id string) string {
	payload, _ := json.Marshal(struct {
		ID   string `json:"id"`
		Key  string `json:"key"`
		Kind string `json:"kind"`
	}{ID: id, Key: key, Kind: kind})
	return "sequence-window.v2." + base64.RawURLEncoding.EncodeToString(payload)
}

func decodeSequenceCursor(value string) (string, string, string, error) {
	if value == "" {
		return "", "", "", nil
	}
	const prefix = "sequence-window.v2."
	if !strings.HasPrefix(value, prefix) {
		return "", "", "", ErrInvalidEditCursor
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil {
		return "", "", "", ErrInvalidEditCursor
	}
	var payload struct {
		ID   string `json:"id"`
		Key  string `json:"key"`
		Kind string `json:"kind"`
	}
	if json.Unmarshal(decoded, &payload) != nil || len(payload.Key) != 48 ||
		(payload.Kind != "caption" && payload.Kind != "clip") {
		return "", "", "", ErrInvalidEditCursor
	}
	if payload.Kind == "caption" {
		if _, err := domain.ParseCaptionID(payload.ID); err != nil {
			return "", "", "", ErrInvalidEditCursor
		}
	} else if _, err := domain.ParseClipID(payload.ID); err != nil {
		return "", "", "", ErrInvalidEditCursor
	}
	return payload.Key, payload.Kind, payload.ID, nil
}
