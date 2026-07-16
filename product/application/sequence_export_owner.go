package application

import "github.com/PerishCode/open-cut/product/domain"

type SequenceExportOwnerKind string

const (
	SequenceExportOwnerAgentRun SequenceExportOwnerKind = "run"
	SequenceExportOwnerCreator  SequenceExportOwnerKind = "creator"
)

type SequenceExportOwner struct {
	Kind SequenceExportOwnerKind
	ID   string
}

func (owner SequenceExportOwner) Validate(actor domain.ActorRef, runID domain.RunID, turnID domain.TurnID) error {
	if actor.Validate() != nil || owner.ID == "" || len(owner.ID) > 128 {
		return ErrSequenceExportInvalid
	}
	switch owner.Kind {
	case SequenceExportOwnerAgentRun:
		if actor.Kind != domain.ActorAgent || runID.IsZero() || turnID.IsZero() || owner.ID != runID.String() {
			return ErrSequenceExportInvalid
		}
	case SequenceExportOwnerCreator:
		if actor.Kind != domain.ActorCreator || !runID.IsZero() || !turnID.IsZero() || owner.ID != actor.IDString() {
			return ErrSequenceExportInvalid
		}
	default:
		return ErrSequenceExportInvalid
	}
	return nil
}
