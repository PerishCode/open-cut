package application

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

var (
	ErrIdentityGeneration    = errors.New("durable identity generation failed")
	ErrRequestIdentityReused = errors.New("request identity was reused with different normalized input")
	ErrAuthorityScopeDenied  = errors.New("product authority does not permit this use case")
	ErrProductStorageInvalid = errors.New("product storage reconciliation input is invalid")
)

const (
	ProjectCreateSchema = "open-cut/application/project-create/v1"
	ActivitySchema      = "open-cut/activity/v1"
)

type Clock interface {
	Now() time.Time
}

type ClockFunc func() time.Time

func (clock ClockFunc) Now() time.Time {
	return clock()
}

type IdentityGenerator interface {
	NewID(context.Context, time.Time) (string, error)
}

type UUIDv7IdentityGenerator struct {
	Random io.Reader
}

func (generator UUIDv7IdentityGenerator) NewID(_ context.Context, at time.Time) (string, error) {
	source := generator.Random
	if source == nil {
		source = rand.Reader
	}
	return domain.GenerateUUIDv7From(at, source)
}

type CreateProjectRecord struct {
	Actor             domain.ActorRef
	InstallationID    string
	ActivityEventID   domain.ActivityEventID
	RequestDigest     domain.Digest
	RequestCanonical  []byte
	ProposalCanonical []byte
	InverseCanonical  []byte
	Genesis           domain.ProjectGenesis
}

type CreateProjectOutcome struct {
	Genesis                    domain.ProjectGenesis
	ProjectActivityCursor      domain.Cursor
	InstallationActivityCursor domain.Cursor
	Replayed                   bool
}

type ProjectRepository interface {
	CreateGenesis(context.Context, CreateProjectRecord) (CreateProjectOutcome, error)
}

type Projects struct {
	repository ProjectRepository
	identities IdentityGenerator
	clock      Clock
}

func NewProjects(repository ProjectRepository, identities IdentityGenerator, clock Clock) (*Projects, error) {
	if repository == nil || identities == nil || clock == nil {
		return nil, fmt.Errorf("projects application dependencies are required")
	}
	return &Projects{repository: repository, identities: identities, clock: clock}, nil
}

type CreateProjectInput struct {
	RequestID domain.RequestID       `json:"requestId" doc:"Creator gesture request identity" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	Name      string                 `json:"name" minLength:"1" maxLength:"200"`
	Format    *domain.SequenceFormat `json:"format,omitempty"`
}

type CreateProjectResult struct {
	Project                    ProjectOverview      `json:"project"`
	ProposalID                 domain.ProposalID    `json:"proposalId" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
	TransactionID              domain.TransactionID `json:"transactionId" format:"uuid" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"`
	RequestDigest              domain.Digest        `json:"requestDigest" format:"sha256-digest" pattern:"^sha256:[0-9a-f]{64}$"`
	ProjectActivityCursor      domain.Cursor        `json:"projectActivityCursor" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	InstallationActivityCursor domain.Cursor        `json:"installationActivityCursor" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	Replayed                   bool                 `json:"replayed"`
}

func (projects *Projects) Create(ctx context.Context, input CreateProjectInput) (CreateProjectResult, error) {
	authority, err := AuthorityFromContext(ctx)
	if err != nil {
		return CreateProjectResult{}, err
	}
	if authority.Surface != AuthorityFirstPartyUI || authority.Actor.Kind != domain.ActorCreator {
		return CreateProjectResult{}, ErrAuthorityScopeDenied
	}
	if _, err := domain.ParseRequestID(input.RequestID.String()); err != nil {
		return CreateProjectResult{}, err
	}
	format := domain.DefaultSequenceFormat()
	if input.Format != nil {
		format = *input.Format
	}
	canonical, err := domain.ProjectGenesisCanonical(input.Name, format)
	if err != nil {
		return CreateProjectResult{}, err
	}
	digest, err := domain.ProjectGenesisDigest(input.Name, format)
	if err != nil {
		return CreateProjectResult{}, err
	}
	now := projects.clock.Now().UTC()
	ids, err := projects.newGenesisIDs(ctx, now)
	if err != nil {
		return CreateProjectResult{}, err
	}
	activityValue, err := projects.identities.NewID(ctx, now)
	if err != nil {
		return CreateProjectResult{}, fmt.Errorf("%w: %v", ErrIdentityGeneration, err)
	}
	installationActivity, err := domain.ParseActivityEventID(activityValue)
	if err != nil {
		return CreateProjectResult{}, fmt.Errorf("%w: %v", ErrIdentityGeneration, err)
	}
	genesis, err := domain.NewProjectGenesis(domain.ProjectGenesisInput{
		Name: input.Name, Format: format, RequestID: input.RequestID, Actor: authority.Actor, CreatedAt: now,
	}, ids)
	if err != nil {
		return CreateProjectResult{}, err
	}
	proposalCanonical, err := domain.ProjectGenesisProposalCanonical(genesis)
	if err != nil {
		return CreateProjectResult{}, err
	}
	inverseCanonical, err := domain.ProjectGenesisInverseCanonical(genesis.Project.ID)
	if err != nil {
		return CreateProjectResult{}, err
	}
	outcome, err := projects.repository.CreateGenesis(ctx, CreateProjectRecord{
		Actor: authority.Actor, InstallationID: authority.InstallationID, ActivityEventID: installationActivity,
		RequestDigest: digest, RequestCanonical: canonical,
		ProposalCanonical: proposalCanonical, InverseCanonical: inverseCanonical, Genesis: genesis,
	})
	if err != nil {
		return CreateProjectResult{}, err
	}
	return CreateProjectResult{
		Project:    projectOverviewFromGenesis(outcome.Genesis, outcome.ProjectActivityCursor),
		ProposalID: outcome.Genesis.Record.ProposalID, TransactionID: outcome.Genesis.Record.TransactionID,
		RequestDigest:              digest,
		ProjectActivityCursor:      outcome.ProjectActivityCursor,
		InstallationActivityCursor: outcome.InstallationActivityCursor,
		Replayed:                   outcome.Replayed,
	}, nil
}

func projectOverviewFromGenesis(genesis domain.ProjectGenesis, cursor domain.Cursor) ProjectOverview {
	project := genesis.Project
	document := project.NarrativeDocuments[0]
	sequence := project.Sequences[0]
	tracks := make([]TrackSummary, 0, len(sequence.Tracks))
	for _, track := range sequence.Tracks {
		tracks = append(tracks, TrackSummary{ID: track.ID, Revision: track.Revision, Type: track.Type, Label: track.Label})
	}
	return ProjectOverview{
		Project: ProjectSummary{
			ID: project.ID, Revision: project.Revision, LifecycleRevision: project.LifecycleRevision,
			Name: project.Name, Status: project.Status, NarrativeDocumentID: document.ID,
			MainSequenceID: sequence.ID,
		},
		NarrativeDocumentRevision: document.Revision, NarrativeRootNodeID: document.RootNodeID,
		MainSequenceRevision: sequence.Revision,
		Format:               sequence.Format, Tracks: tracks, ActivityCursor: cursor,
	}
}

func (projects *Projects) newGenesisIDs(ctx context.Context, at time.Time) (domain.GenesisIDs, error) {
	values := make([]string, 11)
	for index := range values {
		value, err := projects.identities.NewID(ctx, at)
		if err != nil {
			return domain.GenesisIDs{}, fmt.Errorf("%w: %v", ErrIdentityGeneration, err)
		}
		values[index] = value
	}
	project, err := domain.ParseProjectID(values[0])
	if err != nil {
		return domain.GenesisIDs{}, fmt.Errorf("%w: %v", ErrIdentityGeneration, err)
	}
	version, err := domain.ParseProjectVersionID(values[1])
	if err != nil {
		return domain.GenesisIDs{}, fmt.Errorf("%w: %v", ErrIdentityGeneration, err)
	}
	document, err := domain.ParseNarrativeDocumentID(values[2])
	if err != nil {
		return domain.GenesisIDs{}, fmt.Errorf("%w: %v", ErrIdentityGeneration, err)
	}
	root, err := domain.ParseNarrativeNodeID(values[3])
	if err != nil {
		return domain.GenesisIDs{}, fmt.Errorf("%w: %v", ErrIdentityGeneration, err)
	}
	sequence, err := domain.ParseSequenceID(values[4])
	if err != nil {
		return domain.GenesisIDs{}, fmt.Errorf("%w: %v", ErrIdentityGeneration, err)
	}
	video, err := domain.ParseTrackID(values[5])
	if err != nil {
		return domain.GenesisIDs{}, fmt.Errorf("%w: %v", ErrIdentityGeneration, err)
	}
	audio, err := domain.ParseTrackID(values[6])
	if err != nil {
		return domain.GenesisIDs{}, fmt.Errorf("%w: %v", ErrIdentityGeneration, err)
	}
	caption, err := domain.ParseTrackID(values[7])
	if err != nil {
		return domain.GenesisIDs{}, fmt.Errorf("%w: %v", ErrIdentityGeneration, err)
	}
	proposal, err := domain.ParseProposalID(values[8])
	if err != nil {
		return domain.GenesisIDs{}, fmt.Errorf("%w: %v", ErrIdentityGeneration, err)
	}
	transaction, err := domain.ParseTransactionID(values[9])
	if err != nil {
		return domain.GenesisIDs{}, fmt.Errorf("%w: %v", ErrIdentityGeneration, err)
	}
	activityEvent, err := domain.ParseActivityEventID(values[10])
	if err != nil {
		return domain.GenesisIDs{}, fmt.Errorf("%w: %v", ErrIdentityGeneration, err)
	}
	return domain.GenesisIDs{
		Project: project, ProjectVersion: version, NarrativeDocument: document, RootSection: root,
		MainSequence: sequence, VideoTrack: video, AudioTrack: audio, CaptionTrack: caption,
		Proposal: proposal, Transaction: transaction, ActivityEvent: activityEvent,
	}, nil
}
