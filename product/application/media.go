package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	SourceGrantRegisterSchema = "open-cut/application/source-grant-register/v1"
	AssetRegisterSchema       = "open-cut/application/asset-register/v1"
	InitialMediaProducer      = "open-cut-media-v1"
	maximumGrantMaterialBytes = 64 << 10
)

var (
	ErrSourceGrantInvalid   = errors.New("source grant request is invalid")
	ErrSourceGrantNotFound  = errors.New("source grant not found")
	ErrSourceGrantReused    = errors.New("source grant request identity was reused")
	ErrAssetInvalid         = errors.New("asset request is invalid")
	ErrAssetNotFound        = errors.New("asset not found")
	ErrAssetRequestReused   = errors.New("asset request identity was reused")
	ErrAssetAlreadyImported = errors.New("source grant is already registered in this project")
)

type RegisterSourceGrantInput struct {
	RequestID         domain.RequestID
	Platform          string
	Kind              domain.SourceGrantKind
	DisplayName       string
	Observation       domain.SourceObservation
	ProtectedMaterial []byte
}

type RegisterSourceGrantRecord struct {
	ID                domain.SourceGrantID
	InstallationID    string
	CreatorID         domain.CreatorID
	RequestID         domain.RequestID
	InputDigest       domain.Digest
	Platform          string
	Kind              domain.SourceGrantKind
	DisplayName       string
	Observation       domain.SourceObservation
	ProtectedMaterial []byte
	CreatedAt         time.Time
}

type SourceGrantResult struct {
	Grant    domain.SourceGrantSummary `json:"grant"`
	Replayed bool                      `json:"replayed"`
}

type InitialMediaJob struct {
	ID               domain.MediaJobID
	Kind             domain.MediaJobKind
	State            domain.MediaJobState
	Pool             string
	PriorityClass    string
	LogicalKey       string
	ParametersDigest domain.Digest
	ParametersJSON   []byte
	ProducerVersion  string
	Prerequisites    []domain.MediaJobPrerequisite
}

type RegisterAssetInput struct {
	RequestID               domain.RequestID       `json:"requestId" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	SourceGrantID           domain.SourceGrantID   `json:"sourceGrantId"`
	ImportMode              domain.AssetImportMode `json:"importMode" enum:"referenced"`
	ExpectedProjectRevision domain.Revision        `json:"expectedProjectRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
}

type RegisterAssetRecord struct {
	InstallationID              string
	Actor                       domain.ActorRef
	Input                       RegisterAssetInput
	InputDigest                 domain.Digest
	InputCanonical              []byte
	Asset                       domain.AssetState
	Proposal                    domain.EditProposal
	ProposalCanonical           []byte
	ApplicationID               domain.ProposalApplicationID
	Transaction                 domain.EditTransaction
	ProjectActivityEventID      domain.ActivityEventID
	InstallationActivityEventID domain.ActivityEventID
	Jobs                        []InitialMediaJob
	OccurredAt                  time.Time
}

type AssetRegisterResult struct {
	Asset          domain.AssetDetail     `json:"asset"`
	Transaction    domain.EditTransaction `json:"transaction"`
	ActivityCursor domain.Cursor          `json:"activityCursor"`
	Replayed       bool                   `json:"replayed"`
}

type MediaRepository interface {
	MediaFrameSetRepository
	TranscriptReadRepository
	TranscriptSelectionRepository
	RegisterSourceGrant(context.Context, RegisterSourceGrantRecord) (SourceGrantResult, error)
	ReadSourceGrant(context.Context, string, domain.SourceGrantID) (domain.SourceGrantSummary, error)
	RegisterAsset(context.Context, RegisterAssetRecord) (AssetRegisterResult, error)
}

func (media *Media) ReadTranscript(
	ctx context.Context,
	query TranscriptReadQuery,
) (TranscriptReadPage, error) {
	return (&TranscriptReads{repository: media.repository}).Read(ctx, query)
}

func (media *Media) RequestFrames(
	ctx context.Context,
	projectID domain.ProjectID,
	assetID domain.AssetID,
	runID domain.RunID,
	turnID domain.TurnID,
	input RequestMediaFramesInput,
) (MediaFrameSetRequestResult, error) {
	frames := MediaFrames{repository: media.repository, identities: media.identities, clock: media.clock}
	return frames.Request(ctx, projectID, assetID, runID, turnID, input)
}

type Media struct {
	repository MediaRepository
	identities IdentityGenerator
	clock      Clock
}

func NewMedia(repository MediaRepository, identities IdentityGenerator, clock Clock) (*Media, error) {
	if repository == nil || identities == nil || clock == nil {
		return nil, fmt.Errorf("media application dependencies are required")
	}
	return &Media{repository: repository, identities: identities, clock: clock}, nil
}

func (media *Media) RegisterSourceGrant(
	ctx context.Context,
	input RegisterSourceGrantInput,
) (SourceGrantResult, error) {
	authority, err := creatorAuthority(ctx)
	if err != nil {
		return SourceGrantResult{}, err
	}
	if err := validateSourceGrantInput(input); err != nil {
		return SourceGrantResult{}, err
	}
	createdAt := media.clock.Now().UTC()
	id, err := media.newSourceGrantID(ctx, createdAt)
	if err != nil {
		return SourceGrantResult{}, err
	}
	_, digest, err := domain.CanonicalDigest(
		"open-cut/source-grant-register", SourceGrantRegisterSchema,
		struct {
			ActorID           string                   `json:"actorId"`
			InstallationID    string                   `json:"installationId"`
			Platform          string                   `json:"platform"`
			Kind              domain.SourceGrantKind   `json:"kind"`
			DisplayName       string                   `json:"displayName"`
			Observation       domain.SourceObservation `json:"observation"`
			ProtectedMaterial []byte                   `json:"protectedMaterial"`
		}{
			ActorID: authority.Actor.IDString(), InstallationID: authority.InstallationID,
			Platform: input.Platform, Kind: input.Kind, DisplayName: input.DisplayName,
			Observation: input.Observation, ProtectedMaterial: input.ProtectedMaterial,
		},
	)
	if err != nil {
		return SourceGrantResult{}, err
	}
	return media.repository.RegisterSourceGrant(ctx, RegisterSourceGrantRecord{
		ID: id, InstallationID: authority.InstallationID, CreatorID: *authority.Actor.CreatorID,
		RequestID: input.RequestID, InputDigest: digest, Platform: input.Platform,
		Kind: input.Kind, DisplayName: input.DisplayName, Observation: input.Observation,
		ProtectedMaterial: append([]byte(nil), input.ProtectedMaterial...), CreatedAt: createdAt,
	})
}

func (media *Media) RegisterAsset(
	ctx context.Context,
	projectID domain.ProjectID,
	input RegisterAssetInput,
) (AssetRegisterResult, error) {
	authority, err := creatorAuthority(ctx)
	if err != nil {
		return AssetRegisterResult{}, err
	}
	if err := validateRegisterAssetInput(projectID, input); err != nil {
		return AssetRegisterResult{}, err
	}
	grant, err := media.repository.ReadSourceGrant(ctx, authority.InstallationID, input.SourceGrantID)
	if err != nil {
		return AssetRegisterResult{}, err
	}
	if grant.State != domain.SourceGrantActive {
		return AssetRegisterResult{}, ErrSourceGrantNotFound
	}
	now := media.clock.Now().UTC()
	ids, err := media.allocateAssetRegistration(ctx, now)
	if err != nil {
		return AssetRegisterResult{}, err
	}
	requestCanonical, requestDigest, err := domain.CanonicalDigest(
		"open-cut/asset-register-request", AssetRegisterSchema,
		struct {
			Actor     domain.ActorRef    `json:"actor"`
			ProjectID domain.ProjectID   `json:"projectId"`
			Input     RegisterAssetInput `json:"input"`
		}{Actor: authority.Actor, ProjectID: projectID, Input: input},
	)
	if err != nil {
		return AssetRegisterResult{}, err
	}
	revision, _ := domain.NewRevision(1)
	asset := domain.AssetState{
		ID: ids.Asset, Revision: revision, ProjectID: projectID, SourceGrantID: input.SourceGrantID,
		DisplayName: grant.DisplayName, ImportMode: input.ImportMode,
	}
	proposal, proposalCanonical, transaction, err := buildAssetRegisterJournal(
		authority.Actor, projectID, input, asset, ids.Proposal, ids.Transaction, now,
	)
	if err != nil {
		return AssetRegisterResult{}, err
	}
	jobs, err := buildInitialMediaJobs(ids.Jobs, asset.ID)
	if err != nil {
		return AssetRegisterResult{}, err
	}
	return media.repository.RegisterAsset(ctx, RegisterAssetRecord{
		InstallationID: authority.InstallationID, Actor: authority.Actor, Input: input,
		InputDigest: requestDigest, InputCanonical: requestCanonical, Asset: asset,
		Proposal: proposal, ProposalCanonical: proposalCanonical, ApplicationID: ids.Application,
		Transaction: transaction, ProjectActivityEventID: ids.ProjectEvent,
		InstallationActivityEventID: ids.InstallationEvent, Jobs: jobs, OccurredAt: now,
	})
}

func creatorAuthority(ctx context.Context) (Authority, error) {
	authority, err := AuthorityFromContext(ctx)
	if err != nil {
		return Authority{}, err
	}
	if authority.Surface != AuthorityFirstPartyUI || authority.Actor.Kind != domain.ActorCreator {
		return Authority{}, ErrAuthorityScopeDenied
	}
	return authority, nil
}

func validateSourceGrantInput(input RegisterSourceGrantInput) error {
	if _, err := domain.ParseRequestID(input.RequestID.String()); err != nil ||
		(input.Platform != "mac" && input.Platform != "win" && input.Platform != "linux") ||
		(input.Kind != domain.SourceGrantLocalPath && input.Kind != domain.SourceGrantMacBookmark) ||
		!validSafeDisplayName(input.DisplayName) || len(input.ProtectedMaterial) == 0 ||
		len(input.ProtectedMaterial) > maximumGrantMaterialBytes || input.Observation.FileIdentity == "" ||
		len(input.Observation.FileIdentity) > 512 {
		return ErrSourceGrantInvalid
	}
	if input.Kind == domain.SourceGrantMacBookmark && input.Platform != "mac" {
		return ErrSourceGrantInvalid
	}
	return nil
}

func validateRegisterAssetInput(projectID domain.ProjectID, input RegisterAssetInput) error {
	if projectID.IsZero() || input.SourceGrantID.IsZero() || input.ExpectedProjectRevision.Value() < 1 ||
		input.ImportMode != domain.AssetReferenced {
		return ErrAssetInvalid
	}
	if _, err := domain.ParseRequestID(input.RequestID.String()); err != nil {
		return ErrAssetInvalid
	}
	return nil
}

func validSafeDisplayName(value string) bool {
	if value == "" || len([]byte(value)) > 2048 || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, current := range value {
		if current < 0x20 || current == 0x7f {
			return false
		}
	}
	return true
}

type assetRegistrationIDs struct {
	Asset             domain.AssetID
	Proposal          domain.ProposalID
	Application       domain.ProposalApplicationID
	Transaction       domain.TransactionID
	ProjectEvent      domain.ActivityEventID
	InstallationEvent domain.ActivityEventID
	Jobs              []domain.MediaJobID
}

func (media *Media) allocateAssetRegistration(
	ctx context.Context,
	at time.Time,
) (assetRegistrationIDs, error) {
	values := make([]string, 11)
	for index := range values {
		value, err := media.identities.NewID(ctx, at)
		if err != nil {
			return assetRegistrationIDs{}, err
		}
		values[index] = value
	}
	asset, err := domain.ParseAssetID(values[0])
	if err != nil {
		return assetRegistrationIDs{}, err
	}
	proposal, err := domain.ParseProposalID(values[1])
	if err != nil {
		return assetRegistrationIDs{}, err
	}
	applicationID, err := domain.ParseProposalApplicationID(values[2])
	if err != nil {
		return assetRegistrationIDs{}, err
	}
	transaction, err := domain.ParseTransactionID(values[3])
	if err != nil {
		return assetRegistrationIDs{}, err
	}
	projectEvent, err := domain.ParseActivityEventID(values[4])
	if err != nil {
		return assetRegistrationIDs{}, err
	}
	installationEvent, err := domain.ParseActivityEventID(values[5])
	if err != nil {
		return assetRegistrationIDs{}, err
	}
	jobs := make([]domain.MediaJobID, 0, 5)
	for _, value := range values[6:] {
		job, parseErr := domain.ParseMediaJobID(value)
		if parseErr != nil {
			return assetRegistrationIDs{}, parseErr
		}
		jobs = append(jobs, job)
	}
	return assetRegistrationIDs{
		Asset: asset, Proposal: proposal, Application: applicationID, Transaction: transaction,
		ProjectEvent: projectEvent, InstallationEvent: installationEvent, Jobs: jobs,
	}, nil
}

func (media *Media) newSourceGrantID(ctx context.Context, at time.Time) (domain.SourceGrantID, error) {
	value, err := media.identities.NewID(ctx, at)
	if err != nil {
		return domain.SourceGrantID{}, err
	}
	return domain.ParseSourceGrantID(value)
}
