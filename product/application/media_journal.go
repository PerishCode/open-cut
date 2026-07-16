package application

import (
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

func buildAssetRegisterJournal(
	actor domain.ActorRef,
	projectID domain.ProjectID,
	input RegisterAssetInput,
	asset domain.AssetState,
	proposalID domain.ProposalID,
	transactionID domain.TransactionID,
	at time.Time,
) (domain.EditProposal, []byte, domain.EditTransaction, error) {
	operation := domain.NormalizedEditOperation{Type: domain.NormalizedPutAsset, Asset: &asset}
	inverseState := asset
	inverseState.Revision, _ = asset.Revision.Next()
	inverseState.Tombstoned = true
	inverse := domain.NormalizedEditOperation{Type: domain.NormalizedPutAsset, Asset: &inverseState}
	change := domain.EntityRevisionChange{Kind: domain.EntityAsset, ID: asset.ID.String(), After: asset.Revision}
	proposal := domain.EditProposal{
		ID: proposalID, ProjectID: projectID, RequestID: input.RequestID, Actor: actor,
		Intent: "register referenced asset", BaseProjectRevision: input.ExpectedProjectRevision,
		Preconditions: []domain.EntityPrecondition{}, Allocation: []domain.LocalAllocation{{
			Local: mustLocalID("asset"), Kind: domain.EntityAsset, ID: asset.ID.String(),
		}},
		Operations:     []domain.NormalizedEditOperation{operation},
		InversePreview: []domain.NormalizedEditOperation{inverse},
		Changes:        []domain.EntityRevisionChange{change},
		Impact:         domain.EditImpact{Classifier: domain.EditImpactClassifierV1, Class: "reversible-local"},
		Status:         domain.ProposalApplied, CreatedAt: at.UTC(),
	}
	proposalCanonical, proposalDigest, err := domain.CanonicalDigest(
		"open-cut/edit-proposal", domain.AssetRegisterProposalSchema,
		struct {
			Actor               domain.ActorRef                  `json:"actor"`
			Allocation          []domain.LocalAllocation         `json:"allocation"`
			BaseProjectRevision domain.Revision                  `json:"baseProjectRevision"`
			Changes             []domain.EntityRevisionChange    `json:"changes"`
			Impact              domain.EditImpact                `json:"impact"`
			Intent              string                           `json:"intent"`
			Inverse             []domain.NormalizedEditOperation `json:"inverse"`
			Operations          []domain.NormalizedEditOperation `json:"operations"`
			Preconditions       []domain.EntityPrecondition      `json:"preconditions"`
			ProjectID           domain.ProjectID                 `json:"projectId"`
		}{actor, proposal.Allocation, proposal.BaseProjectRevision, proposal.Changes, proposal.Impact,
			proposal.Intent, proposal.InversePreview, proposal.Operations, proposal.Preconditions, projectID},
	)
	if err != nil {
		return domain.EditProposal{}, nil, domain.EditTransaction{}, err
	}
	proposal.Digest = proposalDigest
	projectRevision, err := input.ExpectedProjectRevision.Next()
	if err != nil {
		return domain.EditProposal{}, nil, domain.EditTransaction{}, err
	}
	transaction := domain.EditTransaction{
		ID: transactionID, ProposalID: proposalID, ProjectID: projectID, Actor: actor,
		Intent: proposal.Intent, Operations: proposal.Operations, InverseOperations: proposal.InversePreview,
		Changes: proposal.Changes, CommittedProjectRevision: projectRevision, CommittedAt: at.UTC(),
	}
	_, transactionDigest, err := domain.CanonicalDigest(
		"open-cut/edit-transaction", domain.AssetRegisterTransactionSchema,
		struct {
			Actor                    domain.ActorRef                  `json:"actor"`
			Changes                  []domain.EntityRevisionChange    `json:"changes"`
			CommittedProjectRevision domain.Revision                  `json:"committedProjectRevision"`
			Intent                   string                           `json:"intent"`
			Inverse                  []domain.NormalizedEditOperation `json:"inverse"`
			Operations               []domain.NormalizedEditOperation `json:"operations"`
			ProjectID                domain.ProjectID                 `json:"projectId"`
			ProposalDigest           domain.Digest                    `json:"proposalDigest"`
		}{actor, transaction.Changes, projectRevision, transaction.Intent, transaction.InverseOperations,
			transaction.Operations, projectID, proposalDigest},
	)
	if err != nil {
		return domain.EditProposal{}, nil, domain.EditTransaction{}, err
	}
	transaction.Digest = transactionDigest
	proposal.AppliedTransactionID = &transactionID
	return proposal, proposalCanonical, transaction, nil
}

func buildInitialMediaJobs(ids []domain.MediaJobID, assetID domain.AssetID) ([]InitialMediaJob, error) {
	definitions := []struct {
		kind           domain.MediaJobKind
		pool, priority string
	}{
		{domain.MediaJobIdentify, "io", "foreground"},
		{domain.MediaJobProbe, "interactive-cpu", "foreground"},
		{domain.MediaJobProxy, "cpu", "background"},
		{domain.MediaJobWaveform, "cpu", "background"},
		{domain.MediaJobTranscript, "cpu", "background"},
	}
	if len(ids) != len(definitions) {
		return nil, fmt.Errorf("initial media job identities are incomplete")
	}
	result := make([]InitialMediaJob, 0, len(definitions))
	for index, definition := range definitions {
		profile, err := InitialMediaProfile(definition.kind)
		if err != nil {
			return nil, err
		}
		parameters := InitialMediaJobParameters{
			AssetID: assetID, Kind: definition.kind, Profile: profile,
		}
		if definition.kind == domain.MediaJobProxy {
			parameters.ProxySelection = &SourceProxySelection{Policy: SourceProxySelectionDefault}
		}
		canonical, digest, err := CanonicalInitialMediaJobParameters(parameters)
		if err != nil {
			return nil, err
		}
		result = append(result, InitialMediaJob{
			ID: ids[index], Kind: definition.kind, State: domain.MediaJobBlocked, Pool: definition.pool,
			PriorityClass: definition.priority, LogicalKey: "media/v1/" + assetID.String() + "/" + string(definition.kind) + "/" + digest.String(),
			ParametersDigest: digest, ParametersJSON: canonical, ProducerVersion: InitialMediaProducer,
			Prerequisites: initialMediaPrerequisites(definition.kind, ids),
		})
	}
	return result, nil
}

func initialMediaPrerequisites(kind domain.MediaJobKind, ids []domain.MediaJobID) []domain.MediaJobPrerequisite {
	executor := domain.MediaJobPrerequisite{
		Kind: domain.MediaPrerequisiteExecutor, Capability: "media-executor/" + string(kind),
	}
	switch kind {
	case domain.MediaJobIdentify:
		return []domain.MediaJobPrerequisite{executor}
	case domain.MediaJobProbe:
		producer := ids[0]
		return []domain.MediaJobPrerequisite{{Kind: domain.MediaPrerequisiteFingerprint, JobID: &producer}, executor}
	case domain.MediaJobProxy, domain.MediaJobWaveform:
		producer := ids[1]
		return []domain.MediaJobPrerequisite{{Kind: domain.MediaPrerequisiteFacts, JobID: &producer}, executor}
	case domain.MediaJobTranscript:
		producer := ids[1]
		return []domain.MediaJobPrerequisite{
			{Kind: domain.MediaPrerequisiteFacts, JobID: &producer},
			{Kind: domain.MediaPrerequisiteModel, ResourceID: "whisper-small-multilingual-v1"},
			executor,
		}
	default:
		return nil
	}
}

func mustLocalID(value string) domain.LocalID {
	id, err := domain.ParseLocalID(value)
	if err != nil {
		panic(err)
	}
	return id
}
