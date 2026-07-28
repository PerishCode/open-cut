package domain

import (
	"errors"
	"fmt"
	"time"
)

const (
	EditRequestSchema        = "open-cut/edit-request/v5"
	EditProposalSchema       = "open-cut/edit-proposal/v5"
	EditTransactionSchema    = "open-cut/edit-transaction/v5"
	EditImpactClassifierV1   = "reversible-local-v1"
	MaximumAuthoredTextBytes = 256 << 10
)

var ErrInvalidNarrativeContent = errors.New("invalid narrative content")

var ErrInvalidPlacement = errors.New("invalid clip placement")

type EditEntityKind string

const (
	EntityNarrativeDocument    EditEntityKind = "narrative-document"
	EntityNarrativeNode        EditEntityKind = "narrative-node"
	EntitySequence             EditEntityKind = "sequence"
	EntityTrack                EditEntityKind = "track"
	EntityCaption              EditEntityKind = "caption"
	EntityAlignment            EditEntityKind = "alignment"
	EntityClip                 EditEntityKind = "clip"
	EntityLinkGroup            EditEntityKind = "link-group"
	EntityAsset                EditEntityKind = "asset"
	EntityTranscriptCorrection EditEntityKind = "transcript-correction"
)

type EditOperationType string

const (
	EditInsertSection              EditOperationType = "insert-section"
	EditUpdateSection              EditOperationType = "update-section"
	EditInsertAuthoredText         EditOperationType = "insert-authored-text"
	EditUpdateAuthoredText         EditOperationType = "update-authored-text"
	EditInsertVisualIntent         EditOperationType = "insert-visual-intent"
	EditUpdateVisualIntent         EditOperationType = "update-visual-intent"
	EditInsertNote                 EditOperationType = "insert-note"
	EditUpdateNote                 EditOperationType = "update-note"
	EditMoveNarrativeNode          EditOperationType = "move-narrative-node"
	EditRemoveNarrativeNode        EditOperationType = "remove-narrative-node"
	EditAddCaption                 EditOperationType = "add-caption"
	EditUpdateCaption              EditOperationType = "update-caption"
	EditRemoveCaption              EditOperationType = "remove-caption"
	EditBindAlignment              EditOperationType = "bind-alignment"
	EditMarkAlignmentStale         EditOperationType = "mark-alignment-stale"
	EditUnbindAlignment            EditOperationType = "unbind-alignment"
	EditRemapAlignment             EditOperationType = "remap-alignment"
	EditAddClip                    EditOperationType = "add-clip"
	EditMoveClip                   EditOperationType = "move-clip"
	EditTrimClip                   EditOperationType = "trim-clip"
	EditSplitClip                  EditOperationType = "split-clip"
	EditRemoveClip                 EditOperationType = "remove-clip"
	EditSetClipPlacement           EditOperationType = "set-clip-placement"
	EditLinkClips                  EditOperationType = "link-clips"
	EditUnlinkClips                EditOperationType = "unlink-clips"
	EditAddTranscriptCorrection    EditOperationType = "add-transcript-correction"
	EditUpdateTranscriptCorrection EditOperationType = "update-transcript-correction"
	EditRemoveTranscriptCorrection EditOperationType = "remove-transcript-correction"
	EditInsertSourceExcerpt        EditOperationType = "insert-source-excerpt"
	EditDeriveCaptions             EditOperationType = "derive-captions"
	EditDeriveRoughCut             EditOperationType = "derive-rough-cut"
)

type NormalizedEditOperationType string

const (
	NormalizedPutNarrativeNode        NormalizedEditOperationType = "put-narrative-node"
	NormalizedPutCaption              NormalizedEditOperationType = "put-caption"
	NormalizedPutAlignment            NormalizedEditOperationType = "put-alignment"
	NormalizedPutAsset                NormalizedEditOperationType = "put-asset"
	NormalizedPutClip                 NormalizedEditOperationType = "put-clip"
	NormalizedPutLinkGroup            NormalizedEditOperationType = "put-link-group"
	NormalizedPutTranscriptCorrection NormalizedEditOperationType = "put-transcript-correction"
	NormalizedRestoreProjectVersion   NormalizedEditOperationType = "restore-project-version"
)

type AlignmentStatus string

const (
	AlignmentExact   AlignmentStatus = "exact"
	AlignmentStale   AlignmentStatus = "stale"
	AlignmentUnbound AlignmentStatus = "unbound"
)

type AlignmentTargetType string

const (
	AlignmentTargetCaption  AlignmentTargetType = "caption"
	AlignmentTargetClip     AlignmentTargetType = "clip"
	AlignmentTargetTimeline AlignmentTargetType = "timeline"
)

type ClipMutationScope string

const (
	ClipScopeLinked ClipMutationScope = "linked"
	ClipScopeSingle ClipMutationScope = "single"
)

type ProposalStatus string

const (
	ProposalOpen      ProposalStatus = "open"
	ProposalApplied   ProposalStatus = "applied"
	ProposalStale     ProposalStatus = "stale"
	ProposalCancelled ProposalStatus = "cancelled"
)

type EntityPrecondition struct {
	Kind     EditEntityKind `json:"kind" enum:"narrative-document,narrative-node,sequence,track,caption,alignment,clip,link-group,asset,transcript-correction"`
	ID       string         `json:"id" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
	Revision Revision       `json:"revision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
}

type LocalAllocation struct {
	Local LocalID        `json:"local" pattern:"^[a-z][a-z0-9_-]{0,63}$"`
	Kind  EditEntityKind `json:"kind" enum:"narrative-node,caption,alignment,clip,link-group,asset,transcript-correction"`
	ID    string         `json:"id" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
}

type AuthoredTextPurpose string

const (
	AuthoredTextSpoken   AuthoredTextPurpose = "spoken"
	AuthoredTextOnScreen AuthoredTextPurpose = "on-screen"
)

func (purpose AuthoredTextPurpose) Validate() error {
	if purpose != AuthoredTextSpoken && purpose != AuthoredTextOnScreen {
		return ErrInvalidNarrativeContent
	}
	return nil
}

type VisualIntentPurpose string

const (
	VisualIntentBRoll       VisualIntentPurpose = "b-roll"
	VisualIntentComposition VisualIntentPurpose = "composition"
	VisualIntentReplacement VisualIntentPurpose = "replacement"
)

func (purpose VisualIntentPurpose) Validate() error {
	if purpose != VisualIntentBRoll && purpose != VisualIntentComposition && purpose != VisualIntentReplacement {
		return ErrInvalidNarrativeContent
	}
	return nil
}

type NarrativeSectionState struct {
	ID          NarrativeNodeID     `json:"id"`
	Revision    Revision            `json:"revision"`
	DocumentID  NarrativeDocumentID `json:"documentId"`
	ParentID    *NarrativeNodeID    `json:"parentId,omitempty"`
	AfterNodeID *NarrativeNodeID    `json:"afterNodeId,omitempty"`
	Title       string              `json:"title" minLength:"1" maxLength:"262144"`
	Language    CaptionLanguage     `json:"language" maxLength:"64"`
	Tombstoned  bool                `json:"tombstoned"`
}

type AuthoredTextState struct {
	ID          NarrativeNodeID     `json:"id"`
	Revision    Revision            `json:"revision"`
	DocumentID  NarrativeDocumentID `json:"documentId"`
	ParentID    NarrativeNodeID     `json:"parentId"`
	AfterNodeID *NarrativeNodeID    `json:"afterNodeId,omitempty"`
	Purpose     AuthoredTextPurpose `json:"purpose" enum:"spoken,on-screen"`
	Language    CaptionLanguage     `json:"language" maxLength:"64"`
	Text        string              `json:"text" maxLength:"262144"`
	Tombstoned  bool                `json:"tombstoned"`
}

type VisualIntentState struct {
	ID          NarrativeNodeID     `json:"id"`
	Revision    Revision            `json:"revision"`
	DocumentID  NarrativeDocumentID `json:"documentId"`
	ParentID    NarrativeNodeID     `json:"parentId"`
	AfterNodeID *NarrativeNodeID    `json:"afterNodeId,omitempty"`
	Purpose     VisualIntentPurpose `json:"purpose" enum:"b-roll,composition,replacement"`
	Language    CaptionLanguage     `json:"language" maxLength:"64"`
	Description string              `json:"description" minLength:"1" maxLength:"262144"`
	Tombstoned  bool                `json:"tombstoned"`
}

type NoteState struct {
	ID          NarrativeNodeID     `json:"id"`
	Revision    Revision            `json:"revision"`
	DocumentID  NarrativeDocumentID `json:"documentId"`
	ParentID    NarrativeNodeID     `json:"parentId"`
	AfterNodeID *NarrativeNodeID    `json:"afterNodeId,omitempty"`
	Language    CaptionLanguage     `json:"language" maxLength:"64"`
	Text        string              `json:"text" minLength:"1" maxLength:"262144"`
	Tombstoned  bool                `json:"tombstoned"`
}

type TranscriptCorrectionRevisionRef struct {
	ID       TranscriptCorrectionID `json:"id"`
	Revision Revision               `json:"revision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
}

type TranscriptCorrectionState struct {
	ID              TranscriptCorrectionID `json:"id"`
	Revision        Revision               `json:"revision"`
	AssetID         AssetID                `json:"assetId"`
	ArtifactID      ArtifactID             `json:"transcriptArtifactId"`
	SegmentIDs      []TranscriptSegmentID  `json:"segmentIds" minItems:"1" maxItems:"256" nullable:"false"`
	SourceRange     TimeRange              `json:"sourceRange"`
	ReplacementText string                 `json:"replacementText" minLength:"1" maxLength:"262144"`
	Language        CaptionLanguage        `json:"language" maxLength:"64"`
	Tombstoned      bool                   `json:"tombstoned"`
}

type SourceExcerptTranscriptEvidence struct {
	ArtifactID          ArtifactID                        `json:"artifactId"`
	SourceStreamID      SourceStreamID                    `json:"sourceStreamId"`
	SegmentIDs          []TranscriptSegmentID             `json:"segmentIds" minItems:"1" maxItems:"256" nullable:"false"`
	CorrectionRevisions []TranscriptCorrectionRevisionRef `json:"correctionRevisions" maxItems:"256" nullable:"false"`
}

type SourceExcerptState struct {
	ID                  NarrativeNodeID                 `json:"id"`
	Revision            Revision                        `json:"revision"`
	DocumentID          NarrativeDocumentID             `json:"documentId"`
	ParentID            NarrativeNodeID                 `json:"parentId"`
	AfterNodeID         *NarrativeNodeID                `json:"afterNodeId,omitempty"`
	AssetID             AssetID                         `json:"assetId"`
	AcceptedFingerprint Digest                          `json:"acceptedFingerprint" format:"sha256-digest"`
	SourceRange         TimeRange                       `json:"sourceRange"`
	Language            CaptionLanguage                 `json:"language" maxLength:"64"`
	EffectiveText       string                          `json:"effectiveText" minLength:"1" maxLength:"262144"`
	Evidence            SourceExcerptTranscriptEvidence `json:"evidence"`
	Tombstoned          bool                            `json:"tombstoned"`
}

type SourceExcerptEvidenceStatus string

const (
	SourceExcerptEvidenceExact SourceExcerptEvidenceStatus = "exact"
	SourceExcerptEvidenceStale SourceExcerptEvidenceStatus = "stale"
)

type NarrativeNodeState struct {
	Kind           NarrativeNodeKind           `json:"kind" enum:"section,authored-text,source-excerpt,visual-intent,note"`
	Section        *NarrativeSectionState      `json:"section,omitempty"`
	AuthoredText   *AuthoredTextState          `json:"authoredText,omitempty"`
	SourceExcerpt  *SourceExcerptState         `json:"sourceExcerpt,omitempty"`
	VisualIntent   *VisualIntentState          `json:"visualIntent,omitempty"`
	Note           *NoteState                  `json:"note,omitempty"`
	EvidenceStatus SourceExcerptEvidenceStatus `json:"evidenceStatus,omitempty" enum:"exact,stale"`
}

func (state NarrativeNodeState) ID() NarrativeNodeID {
	if state.Section != nil {
		return state.Section.ID
	}
	if state.AuthoredText != nil {
		return state.AuthoredText.ID
	}
	if state.SourceExcerpt != nil {
		return state.SourceExcerpt.ID
	}
	if state.VisualIntent != nil {
		return state.VisualIntent.ID
	}
	if state.Note != nil {
		return state.Note.ID
	}
	return NarrativeNodeID{}
}

func (state NarrativeNodeState) RevisionValue() Revision {
	if state.Section != nil {
		return state.Section.Revision
	}
	if state.AuthoredText != nil {
		return state.AuthoredText.Revision
	}
	if state.SourceExcerpt != nil {
		return state.SourceExcerpt.Revision
	}
	if state.VisualIntent != nil {
		return state.VisualIntent.Revision
	}
	if state.Note != nil {
		return state.Note.Revision
	}
	return 0
}

func (state NarrativeNodeState) IsTombstoned() bool {
	if state.Section != nil {
		return state.Section.Tombstoned
	}
	if state.AuthoredText != nil {
		return state.AuthoredText.Tombstoned
	}
	if state.SourceExcerpt != nil {
		return state.SourceExcerpt.Tombstoned
	}
	if state.VisualIntent != nil {
		return state.VisualIntent.Tombstoned
	}
	if state.Note != nil {
		return state.Note.Tombstoned
	}
	return false
}

type CaptionState struct {
	ID               CaptionID                `json:"id"`
	Revision         Revision                 `json:"revision"`
	SequenceID       SequenceID               `json:"sequenceId"`
	TrackID          TrackID                  `json:"trackId"`
	Range            TimeRange                `json:"range"`
	Language         CaptionLanguage          `json:"language" maxLength:"64"`
	Text             string                   `json:"text" maxLength:"262144"`
	Provenance       CaptionProvenance        `json:"provenance"`
	ProvenanceStatus *CaptionProvenanceStatus `json:"provenanceStatus,omitempty"`
	Tombstoned       bool                     `json:"tombstoned"`
}

type CaptionAlignmentTarget struct {
	CaptionID       CaptionID `json:"captionId"`
	CaptionRevision Revision  `json:"captionRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	LocalRange      TimeRange `json:"localRange"`
}

type ClipAlignmentTarget struct {
	ClipID       ClipID    `json:"clipId"`
	ClipRevision Revision  `json:"clipRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	LocalRange   TimeRange `json:"localRange"`
}

type TimelineAlignmentTarget struct {
	SequenceRevision Revision  `json:"sequenceRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Range            TimeRange `json:"range"`
}

type AlignmentTarget struct {
	Type     AlignmentTargetType      `json:"type" enum:"caption,clip,timeline"`
	Caption  *CaptionAlignmentTarget  `json:"caption,omitempty"`
	Clip     *ClipAlignmentTarget     `json:"clip,omitempty"`
	Timeline *TimelineAlignmentTarget `json:"timeline,omitempty"`
}

type AlignmentState struct {
	ID                    AlignmentID       `json:"id"`
	Revision              Revision          `json:"revision"`
	NarrativeNodeID       NarrativeNodeID   `json:"narrativeNodeId"`
	NarrativeNodeRevision Revision          `json:"narrativeNodeRevision"`
	SequenceID            SequenceID        `json:"sequenceId"`
	Targets               []AlignmentTarget `json:"targets" minItems:"1" maxItems:"64" nullable:"false"`
	Status                AlignmentStatus   `json:"status" enum:"exact,stale,unbound"`
}

type ClipState struct {
	ID             ClipID         `json:"id"`
	Revision       Revision       `json:"revision"`
	SequenceID     SequenceID     `json:"sequenceId"`
	TrackID        TrackID        `json:"trackId"`
	AssetID        AssetID        `json:"assetId"`
	SourceStreamID SourceStreamID `json:"sourceStreamId"`
	SourceRange    TimeRange      `json:"sourceRange"`
	TimelineRange  TimeRange      `json:"timelineRange"`
	Enabled        bool           `json:"enabled"`
	LinkGroupID    *LinkGroupID   `json:"linkGroupId,omitempty"`
	Placement      *ClipPlacement `json:"placement,omitempty"`
	Tombstoned     bool           `json:"tombstoned"`
}

// ClipPlacement is a visual clip's static creative transform, expressed in the
// exact vocabulary the render plan already composes with: a source crop window
// in basis points, exact-rational scale and canvas translation, and opacity in
// basis points. Absent placement means identity. Rotation and anchor deliberately
// stay out until the plan schema versions them.
type ClipPlacement struct {
	CropXBasisPoints      uint16        `json:"cropXBasisPoints" maximum:"9999"`
	CropYBasisPoints      uint16        `json:"cropYBasisPoints" maximum:"9999"`
	CropWidthBasisPoints  uint16        `json:"cropWidthBasisPoints" minimum:"1" maximum:"10000"`
	CropHeightBasisPoints uint16        `json:"cropHeightBasisPoints" minimum:"1" maximum:"10000"`
	ScaleX                ExactRational `json:"scaleX"`
	ScaleY                ExactRational `json:"scaleY"`
	TranslateX            ExactRational `json:"translateX"`
	TranslateY            ExactRational `json:"translateY"`
	OpacityBasisPoints    uint16        `json:"opacityBasisPoints" maximum:"10000"`
}

// ClipPlacementScaleLimit bounds |scale| to a sane creative magnitude; the
// renderer is exact but a thousandfold blowup is an input mistake, not intent.
const ClipPlacementScaleLimit = 100

// ClipPlacementTranslateLimit bounds |translate| in canvas units (1 = one full
// canvas edge); ten canvas widths off-screen is an input mistake.
const ClipPlacementTranslateLimit = 10

func (placement ClipPlacement) Validate() error {
	if placement.CropWidthBasisPoints < 1 || placement.CropHeightBasisPoints < 1 ||
		uint32(placement.CropXBasisPoints)+uint32(placement.CropWidthBasisPoints) > 10_000 ||
		uint32(placement.CropYBasisPoints)+uint32(placement.CropHeightBasisPoints) > 10_000 {
		return fmt.Errorf("%w: placement crop window must stay inside [0,10000] basis points", ErrInvalidPlacement)
	}
	if placement.OpacityBasisPoints > 10_000 {
		return fmt.Errorf("%w: placement opacity is at most 10000 basis points", ErrInvalidPlacement)
	}
	for _, scale := range []ExactRational{placement.ScaleX, placement.ScaleY} {
		if scale.Validate() != nil || scale.Value <= 0 ||
			int64(scale.Value) > int64(scale.Scale)*ClipPlacementScaleLimit {
			return fmt.Errorf("%w: placement scale must be positive and at most %dx", ErrInvalidPlacement, ClipPlacementScaleLimit)
		}
	}
	for _, translate := range []ExactRational{placement.TranslateX, placement.TranslateY} {
		if translate.Validate() != nil ||
			absInt64(int64(translate.Value)) > uint64(translate.Scale)*ClipPlacementTranslateLimit {
			return fmt.Errorf("%w: placement translation must stay within %d canvas units", ErrInvalidPlacement, ClipPlacementTranslateLimit)
		}
	}
	return nil
}

// IdentityClipPlacement is the placement every clip has when none is stored.
func IdentityClipPlacement() ClipPlacement {
	one := ExactRational{Value: 1, Scale: 1}
	zero := ExactRational{Value: 0, Scale: 1}
	return ClipPlacement{
		CropXBasisPoints: 0, CropYBasisPoints: 0,
		CropWidthBasisPoints: 10_000, CropHeightBasisPoints: 10_000,
		ScaleX: one, ScaleY: one, TranslateX: zero, TranslateY: zero,
		OpacityBasisPoints: 10_000,
	}
}

func (placement ClipPlacement) IsIdentity() bool {
	return placement == IdentityClipPlacement()
}

type LinkGroupState struct {
	ID         LinkGroupID `json:"id"`
	Revision   Revision    `json:"revision"`
	SequenceID SequenceID  `json:"sequenceId"`
	Tombstoned bool        `json:"tombstoned"`
}

type NormalizedEditOperation struct {
	Type                 NormalizedEditOperationType `json:"type" enum:"put-narrative-node,put-transcript-correction,put-caption,put-alignment,put-asset,put-clip,put-link-group,restore-project-version"`
	NarrativeNode        *NarrativeNodeState         `json:"narrativeNode,omitempty"`
	TranscriptCorrection *TranscriptCorrectionState  `json:"transcriptCorrection,omitempty"`
	Caption              *CaptionState               `json:"caption,omitempty"`
	Alignment            *AlignmentState             `json:"alignment,omitempty"`
	Asset                *AssetState                 `json:"asset,omitempty"`
	Clip                 *ClipState                  `json:"clip,omitempty"`
	LinkGroup            *LinkGroupState             `json:"linkGroup,omitempty"`
	ProjectVersion       *ProjectVersionRestoreRef   `json:"projectVersion,omitempty"`
}

type ProjectVersionRestoreRef struct {
	ID     ProjectVersionID `json:"id"`
	Digest Digest           `json:"digest" format:"sha256-digest"`
}

type EntityRevisionChange struct {
	Kind       EditEntityKind `json:"kind" enum:"narrative-document,narrative-node,sequence,track,caption,alignment,clip,link-group,asset,transcript-correction"`
	ID         string         `json:"id"`
	Before     *Revision      `json:"before,omitempty"`
	After      Revision       `json:"after"`
	Tombstoned bool           `json:"tombstoned,omitempty"`
}

type EditImpact struct {
	Classifier       string `json:"classifier" enum:"reversible-local-v1"`
	Class            string `json:"class" enum:"reversible-local"`
	RequiresApproval bool   `json:"requiresApproval"`
}

type EditProposal struct {
	ID                   ProposalID                `json:"id"`
	ProjectID            ProjectID                 `json:"projectId"`
	SequenceID           *SequenceID               `json:"sequenceId,omitempty"`
	RunID                *RunID                    `json:"runId,omitempty"`
	TurnID               *TurnID                   `json:"turnId,omitempty"`
	RequestID            RequestID                 `json:"requestId"`
	Actor                ActorRef                  `json:"actor"`
	Intent               string                    `json:"intent"`
	BaseProjectRevision  Revision                  `json:"baseProjectRevision"`
	Preconditions        []EntityPrecondition      `json:"preconditions" maxItems:"2048" nullable:"false"`
	Allocation           []LocalAllocation         `json:"allocation" maxItems:"1024" nullable:"false"`
	Operations           []NormalizedEditOperation `json:"operations" maxItems:"512" nullable:"false"`
	InversePreview       []NormalizedEditOperation `json:"inversePreview" maxItems:"512" nullable:"false"`
	Changes              []EntityRevisionChange    `json:"changes" maxItems:"2048" nullable:"false"`
	Impact               EditImpact                `json:"impact"`
	Digest               Digest                    `json:"digest"`
	Status               ProposalStatus            `json:"status" enum:"open,applied,stale,cancelled"`
	CreatedAt            time.Time                 `json:"createdAt"`
	AppliedTransactionID *TransactionID            `json:"appliedTransactionId,omitempty"`
}

type EditTransaction struct {
	ID                       TransactionID             `json:"id"`
	ProposalID               ProposalID                `json:"proposalId"`
	ProjectID                ProjectID                 `json:"projectId"`
	Actor                    ActorRef                  `json:"actor"`
	Intent                   string                    `json:"intent"`
	Operations               []NormalizedEditOperation `json:"operations" maxItems:"512" nullable:"false"`
	InverseOperations        []NormalizedEditOperation `json:"inverseOperations" maxItems:"512" nullable:"false"`
	Changes                  []EntityRevisionChange    `json:"changes" maxItems:"2048" nullable:"false"`
	CommittedProjectRevision Revision                  `json:"committedProjectRevision"`
	Digest                   Digest                    `json:"digest"`
	UndoesTransactionID      *TransactionID            `json:"undoesTransactionId,omitempty"`
	CommittedAt              time.Time                 `json:"committedAt"`
}
