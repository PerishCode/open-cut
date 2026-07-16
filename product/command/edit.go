package command

import (
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type NarrativeShowInput struct {
	DocumentID domain.NarrativeDocumentID `json:"documentId" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$" doc:"Narrative document to inspect"`
	ParentID   domain.NarrativeNodeID     `json:"parentId" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$" doc:"Section whose authored-text children are returned"`
	After      string                     `json:"after,omitempty" maxLength:"512" doc:"Opaque query-local continuation cursor"`
	Limit      uint16                     `json:"limit,omitempty" minimum:"1" maximum:"200" default:"50" doc:"Maximum authored-text nodes to return"`
}

type SequenceShowInput struct {
	TrackID *domain.TrackID  `json:"trackId,omitempty" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$" doc:"Optional track filter"`
	Range   domain.TimeRange `json:"range" doc:"Half-open Sequence time window"`
	After   string           `json:"after,omitempty" maxLength:"512" doc:"Opaque query-local continuation cursor"`
	Limit   uint16           `json:"limit,omitempty" minimum:"1" maximum:"512" default:"100" doc:"Maximum captions to return"`
}

type EntityShowInput struct {
	Kind domain.EditEntityKind `json:"kind" enum:"narrative-node,transcript-correction,caption,alignment,clip,link-group" doc:"Editable entity kind"`
	ID   string                `json:"id" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$" doc:"Editable entity identity"`
}

type EditShowInput struct {
	ProposalID domain.ProposalID `json:"proposalId" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$" doc:"Proposal to inspect"`
}

type EditHistoryInput struct {
	After domain.Revision `json:"after,omitempty" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$" doc:"Return transactions committed after this Project revision"`
	Limit uint16          `json:"limit,omitempty" minimum:"1" maximum:"100" default:"50" doc:"Maximum transactions to return"`
}

type CaptionDeriveInput = application.CaptionDerivationPreviewInput
type RoughCutDeriveInput = application.RoughCutDerivationPreviewInput

type EditProposeInput = application.EditProposeInput
type EditApplyInput = application.EditApplyInput
type EditUndoInput = application.EditUndoInput

type NarrativeShowData = application.NarrativeSubtreePage
type SequenceShowData = application.SequenceWindowPage
type EntityShowData = application.EditEntityDetail

type EditShowData struct {
	Proposal       domain.EditProposal `json:"proposal"`
	ActivityCursor domain.Cursor       `json:"activityCursor"`
}

type EditHistoryData = application.TransactionHistoryPage
type CaptionDeriveData = application.CaptionDerivationPreview
type RoughCutDeriveData = application.RoughCutDerivationPreview
type EditProposalData = application.EditProposalResult
type EditCommitData = application.EditCommitResult
