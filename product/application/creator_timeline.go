package application

import "github.com/PerishCode/open-cut/product/domain"

type CreatorTimelineGestureKind string

const (
	CreatorTimelineMove   CreatorTimelineGestureKind = "move"
	CreatorTimelineTrim   CreatorTimelineGestureKind = "trim"
	CreatorTimelineSplit  CreatorTimelineGestureKind = "split"
	CreatorTimelineRemove CreatorTimelineGestureKind = "remove"
)

type CreatorTimelineAlignmentHandling string

const (
	CreatorTimelinePreserveAlignment CreatorTimelineAlignmentHandling = "preserve-if-provable"
	CreatorTimelineStaleAlignment    CreatorTimelineAlignmentHandling = "mark-stale"
	CreatorTimelineUnbindAlignment   CreatorTimelineAlignmentHandling = "unbind"
)

type CreatorTimelineGesturePreviewInput struct {
	Kind              CreatorTimelineGestureKind       `json:"kind" enum:"move,trim,split,remove"`
	ClipID            domain.ClipID                    `json:"clipId"`
	ClipRevision      domain.Revision                  `json:"clipRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Scope             domain.ClipMutationScope         `json:"scope" enum:"linked,single"`
	AlignmentHandling CreatorTimelineAlignmentHandling `json:"alignmentHandling" enum:"preserve-if-provable,mark-stale,unbind"`
	TrackID           *domain.TrackID                  `json:"trackId,omitempty"`
	TrackRevision     *domain.Revision                 `json:"trackRevision,omitempty" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	TimelineStart     *domain.RationalTime             `json:"timelineStart,omitempty"`
	SourceRange       *domain.TimeRange                `json:"sourceRange,omitempty"`
	TimelineRange     *domain.TimeRange                `json:"timelineRange,omitempty"`
	SplitAt           *domain.RationalTime             `json:"splitAt,omitempty"`
	LocalPrefix       *domain.LocalID                  `json:"localPrefix,omitempty" pattern:"^[a-z][a-z0-9_-]{0,39}$"`
}

type CreatorTimelineGesturePreviewQuery struct {
	ProjectID  domain.ProjectID
	SequenceID domain.SequenceID
	Actor      domain.ActorRef
	Input      CreatorTimelineGesturePreviewInput
}

type CreatorTimelineAlignmentEffect struct {
	AlignmentID domain.AlignmentID               `json:"alignmentId"`
	Revision    domain.Revision                  `json:"revision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Handling    CreatorTimelineAlignmentHandling `json:"handling" enum:"preserve-if-provable,mark-stale,unbind"`
	TargetCount int                              `json:"targetCount" minimum:"0" maximum:"64"`
}

type CreatorTimelineClipPlacement struct {
	Revision      domain.Revision  `json:"revision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	TrackID       domain.TrackID   `json:"trackId"`
	SourceRange   domain.TimeRange `json:"sourceRange"`
	TimelineRange domain.TimeRange `json:"timelineRange"`
	Linked        bool             `json:"linked"`
}

type CreatorTimelineClipEffectOutcome string

const (
	CreatorTimelineClipUpdated CreatorTimelineClipEffectOutcome = "updated"
	CreatorTimelineClipSplit   CreatorTimelineClipEffectOutcome = "split"
	CreatorTimelineClipRemoved CreatorTimelineClipEffectOutcome = "removed"
)

type CreatorTimelineClipEffect struct {
	ClipID  domain.ClipID                    `json:"clipId"`
	Before  CreatorTimelineClipPlacement     `json:"before"`
	Outcome CreatorTimelineClipEffectOutcome `json:"outcome" enum:"updated,split,removed"`
	After   *CreatorTimelineClipPlacement    `json:"after,omitempty"`
	Left    *CreatorTimelineClipPlacement    `json:"left,omitempty"`
	Right   *CreatorTimelineClipPlacement    `json:"right,omitempty"`
}

type CreatorTimelineGesturePreview struct {
	BaseProjectRevision domain.Revision                  `json:"baseProjectRevision"`
	Preconditions       []domain.EntityPrecondition      `json:"preconditions" maxItems:"2048" nullable:"false"`
	Operations          []EditOperationInput             `json:"operations" maxItems:"512" nullable:"false"`
	Kind                CreatorTimelineGestureKind       `json:"kind" enum:"move,trim,split,remove"`
	Scope               domain.ClipMutationScope         `json:"scope" enum:"linked,single"`
	SeedClipID          domain.ClipID                    `json:"seedClipId"`
	AffectedClipIDs     []domain.ClipID                  `json:"affectedClipIds" maxItems:"64" nullable:"false"`
	CreatedClipLocals   []domain.LocalID                 `json:"createdClipLocals" maxItems:"128" nullable:"false"`
	ClipEffects         []CreatorTimelineClipEffect      `json:"clipEffects" minItems:"1" maxItems:"64" nullable:"false"`
	AlignmentEffects    []CreatorTimelineAlignmentEffect `json:"alignmentEffects" maxItems:"2048" nullable:"false"`
	OutputDigest        domain.Digest                    `json:"outputDigest" format:"sha256-digest"`
	ActivityCursor      domain.Cursor                    `json:"activityCursor"`
}

type CreatorTimelineBlockedReason string

const (
	CreatorTimelineNoChange           CreatorTimelineBlockedReason = "no-change"
	CreatorTimelineScopeUnavailable   CreatorTimelineBlockedReason = "scope-unavailable"
	CreatorTimelineTrackIncompatible  CreatorTimelineBlockedReason = "track-incompatible"
	CreatorTimelineRangeInvalid       CreatorTimelineBlockedReason = "range-invalid"
	CreatorTimelineTrackCollision     CreatorTimelineBlockedReason = "track-collision"
	CreatorTimelinePreserveUnprovable CreatorTimelineBlockedReason = "alignment-preserve-unprovable"
	CreatorTimelineClosureLimit       CreatorTimelineBlockedReason = "closure-limit"
)

type CreatorTimelineBlockedRecovery string

const (
	CreatorTimelineChooseSingle CreatorTimelineBlockedRecovery = "choose-single"
	CreatorTimelineChooseTrack  CreatorTimelineBlockedRecovery = "choose-compatible-track"
	CreatorTimelineChangeTarget CreatorTimelineBlockedRecovery = "change-target"
	CreatorTimelineMarkStale    CreatorTimelineBlockedRecovery = "mark-stale"
	CreatorTimelineUnbind       CreatorTimelineBlockedRecovery = "unbind"
	CreatorTimelineReduceScope  CreatorTimelineBlockedRecovery = "reduce-scope"
)

type CreatorTimelineGestureBlocked struct {
	BaseProjectRevision domain.Revision                  `json:"baseProjectRevision"`
	Kind                CreatorTimelineGestureKind       `json:"kind" enum:"move,trim,split,remove"`
	Scope               domain.ClipMutationScope         `json:"scope" enum:"linked,single"`
	SeedClipID          domain.ClipID                    `json:"seedClipId"`
	Reason              CreatorTimelineBlockedReason     `json:"reason" enum:"no-change,scope-unavailable,track-incompatible,range-invalid,track-collision,alignment-preserve-unprovable,closure-limit"`
	SubjectClipIDs      []domain.ClipID                  `json:"subjectClipIds" maxItems:"64" nullable:"false"`
	SubjectAlignmentIDs []domain.AlignmentID             `json:"subjectAlignmentIds" maxItems:"512" nullable:"false"`
	Recoveries          []CreatorTimelineBlockedRecovery `json:"recoveries" maxItems:"4" nullable:"false" enum:"choose-single,choose-compatible-track,change-target,mark-stale,unbind,reduce-scope"`
	ActivityCursor      domain.Cursor                    `json:"activityCursor"`
}

type CreatorTimelineGesturePreviewStatus string

const (
	CreatorTimelinePreviewReady   CreatorTimelineGesturePreviewStatus = "ready"
	CreatorTimelinePreviewBlocked CreatorTimelineGesturePreviewStatus = "blocked"
)

type CreatorTimelineGesturePreviewResult struct {
	Status  CreatorTimelineGesturePreviewStatus `json:"status" enum:"ready,blocked"`
	Ready   *CreatorTimelineGesturePreview      `json:"ready,omitempty"`
	Blocked *CreatorTimelineGestureBlocked      `json:"blocked,omitempty"`
}
