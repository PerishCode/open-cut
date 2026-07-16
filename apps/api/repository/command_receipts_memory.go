package repository

import (
	"context"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *MemoryProjects) RecordCommandReceipt(
	_ context.Context,
	record application.RecordCommandReceipt,
) (application.CommandReceipt, error) {
	if err := validateCommandReceiptRecord(record); err != nil {
		return application.CommandReceipt{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	for _, receipts := range repository.commandReceipts {
		for _, existing := range receipts {
			if existing.ID == record.Receipt.ID {
				if !sameCommandReceiptInvocation(existing, record.Receipt) {
					return application.CommandReceipt{}, application.ErrCommandReceiptInvalid
				}
				return existing, nil
			}
			if record.Receipt.RequestID != nil && existing.RequestID != nil &&
				*record.Receipt.RequestID == *existing.RequestID {
				run, runExists := repository.runs[existing.RunID.String()]
				if !runExists || run.Actor != record.Actor || !sameLogicalCommandReceipt(existing, record.Receipt) {
					return application.CommandReceipt{}, application.ErrCommandReceiptInvalid
				}
				return existing, nil
			}
		}
	}
	run, exists := repository.runs[record.Receipt.RunID.String()]
	if !exists || run.ProjectID != record.Receipt.ProjectID || run.Actor != record.Actor ||
		run.CurrentTurn.ID != record.Receipt.TurnID {
		return application.CommandReceipt{}, application.ErrCommandReceiptNotFound
	}
	key := memoryCommandReceiptKey(record.Receipt.ProjectID, record.Receipt.RunID, record.Receipt.TurnID)
	receipt := record.Receipt
	ordinal, err := domain.NewCursor(uint64(len(repository.commandReceipts[key]) + 1))
	if err != nil {
		return application.CommandReceipt{}, err
	}
	receipt.Ordinal = ordinal
	receipt.ResultRefs = append([]application.CommandReceiptRef(nil), receipt.ResultRefs...)
	repository.commandReceipts[key] = append(repository.commandReceipts[key], receipt)
	return receipt, nil
}

func (repository *MemoryProjects) FindCommandReceipt(
	_ context.Context,
	actor domain.ActorRef,
	requestID domain.RequestID,
) (application.CommandReceipt, bool, error) {
	if actor.Validate() != nil || actor.Kind != domain.ActorAgent {
		return application.CommandReceipt{}, false, application.ErrCommandReceiptInvalid
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	for _, receipts := range repository.commandReceipts {
		for _, receipt := range receipts {
			if receipt.RequestID == nil || *receipt.RequestID != requestID {
				continue
			}
			run, exists := repository.runs[receipt.RunID.String()]
			if !exists || run.Actor != actor {
				continue
			}
			copy := receipt
			copy.ResultRefs = append([]application.CommandReceiptRef(nil), receipt.ResultRefs...)
			return copy, true, nil
		}
	}
	return application.CommandReceipt{}, false, nil
}

func (repository *MemoryProjects) ListCommandReceipts(
	_ context.Context,
	projectID domain.ProjectID,
	runID domain.RunID,
	turnID domain.TurnID,
	after domain.Cursor,
	limit uint32,
) (application.TurnReceiptPage, error) {
	if projectID.IsZero() || runID.IsZero() || turnID.IsZero() || limit == 0 ||
		limit > application.MaximumCommandReceiptPage {
		return application.TurnReceiptPage{}, application.ErrCommandReceiptInvalid
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	run, exists := repository.runs[runID.String()]
	if !exists || run.ProjectID != projectID || run.CurrentTurn.ID != turnID {
		return application.TurnReceiptPage{}, application.ErrCommandReceiptNotFound
	}
	stored := repository.commandReceipts[memoryCommandReceiptKey(projectID, runID, turnID)]
	receipts := make([]application.CommandReceipt, 0, limit)
	for _, receipt := range stored {
		if receipt.Ordinal.Value() <= after.Value() {
			continue
		}
		if len(receipts) == int(limit) {
			break
		}
		copy := receipt
		copy.ResultRefs = append([]application.CommandReceiptRef(nil), receipt.ResultRefs...)
		receipts = append(receipts, copy)
	}
	var next *domain.Cursor
	if len(receipts) > 0 {
		last := receipts[len(receipts)-1].Ordinal
		if int(last.Value()) < len(stored) {
			next = &last
		}
	}
	return application.TurnReceiptPage{
		ProjectID: projectID,
		RunID:     runID,
		TurnID:    turnID,
		Receipts:  receipts,
		NextAfter: next,
	}, nil
}

func memoryCommandReceiptKey(projectID domain.ProjectID, runID domain.RunID, turnID domain.TurnID) string {
	return projectID.String() + "\x00" + runID.String() + "\x00" + turnID.String()
}
