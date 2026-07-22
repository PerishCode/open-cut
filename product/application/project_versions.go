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

const ProjectVersionRequestSchema = "open-cut/application/project-version-request/v1"

var (
	ErrProjectVersionInvalid       = errors.New("project version input is invalid")
	ErrProjectVersionNotFound      = errors.New("project version not found")
	ErrProjectVersionRequestReused = errors.New("project version request identity was reused")
)

type ProjectVersionSource string

const (
	ProjectVersionGenesis    ProjectVersionSource = "genesis"
	ProjectVersionManual     ProjectVersionSource = "manual"
	ProjectVersionAgentTurn  ProjectVersionSource = "agent-turn"
	ProjectVersionPreRestore ProjectVersionSource = "pre-restore"
)

type ProjectVersionRetention string

const (
	ProjectVersionAutomatic ProjectVersionRetention = "automatic"
	ProjectVersionRetain    ProjectVersionRetention = "manual"
	ProjectVersionPinned    ProjectVersionRetention = "pinned"
)

type ProjectVersion struct {
	ID                      domain.ProjectVersionID  `json:"id" format:"uuid"`
	ProjectID               domain.ProjectID         `json:"projectId" format:"uuid"`
	ParentVersionID         *domain.ProjectVersionID `json:"parentVersionId,omitempty" format:"uuid"`
	CapturedProjectRevision domain.Revision          `json:"capturedProjectRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Source                  ProjectVersionSource     `json:"source" enum:"genesis,manual,agent-turn,pre-restore"`
	Name                    string                   `json:"name,omitempty" maxLength:"200"`
	TriggerKind             string                   `json:"triggerKind,omitempty" enum:"turn,version"`
	TriggerID               string                   `json:"triggerId,omitempty" format:"uuid"`
	Digest                  domain.Digest            `json:"digest" format:"sha256-digest"`
	ByteSize                domain.UInt64            `json:"byteSize" format:"uint64-decimal"`
	Retention               ProjectVersionRetention  `json:"retention" enum:"automatic,manual,pinned"`
	CreatedAt               time.Time                `json:"createdAt"`
}

type ProjectVersionPage struct {
	Versions       []ProjectVersion         `json:"versions" maxItems:"50" nullable:"false"`
	NextBefore     *domain.ProjectVersionID `json:"nextBefore,omitempty" format:"uuid"`
	ActivityCursor domain.Cursor            `json:"activityCursor" format:"uint64-decimal"`
}

type CreateProjectVersionInput struct {
	RequestID domain.RequestID `json:"requestId" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	Name      string           `json:"name" minLength:"1" maxLength:"200"`
}

type RestoreProjectVersionInput struct {
	RequestID               domain.RequestID `json:"requestId" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	ExpectedProjectRevision domain.Revision  `json:"expectedProjectRevision" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
}

type CreateProjectVersionRecord struct {
	ID               domain.ProjectVersionID
	ProjectID        domain.ProjectID
	Creator          domain.ActorRef
	RequestID        domain.RequestID
	RequestDigest    domain.Digest
	RequestCanonical []byte
	Name             string
	ActivityEventID  domain.ActivityEventID
	CreatedAt        time.Time
}

type RestoreProjectVersionRecord struct {
	ProjectID               domain.ProjectID
	VersionID               domain.ProjectVersionID
	SafetyVersionID         domain.ProjectVersionID
	Creator                 domain.ActorRef
	RequestID               domain.RequestID
	RequestDigest           domain.Digest
	RequestCanonical        []byte
	ExpectedProjectRevision domain.Revision
	ProposalID              domain.ProposalID
	ApplicationID           domain.ProposalApplicationID
	TransactionID           domain.TransactionID
	ActivityEventID         domain.ActivityEventID
	OccurredAt              time.Time
}

type CreateProjectVersionResult struct {
	Version        ProjectVersion `json:"version"`
	ActivityCursor domain.Cursor  `json:"activityCursor" format:"uint64-decimal"`
	Replayed       bool           `json:"replayed"`
}

type RestoreProjectVersionResult struct {
	Version                  ProjectVersion       `json:"version"`
	SafetyVersion            ProjectVersion       `json:"safetyVersion"`
	TransactionID            domain.TransactionID `json:"transactionId" format:"uuid"`
	CommittedProjectRevision domain.Revision      `json:"committedProjectRevision" format:"uint64-decimal"`
	ActivityCursor           domain.Cursor        `json:"activityCursor" format:"uint64-decimal"`
	Replayed                 bool                 `json:"replayed"`
}

type ProjectVersionRepository interface {
	CreateProjectVersion(context.Context, CreateProjectVersionRecord) (CreateProjectVersionResult, error)
	ListProjectVersions(context.Context, domain.ProjectID, domain.ProjectVersionID, uint16) (ProjectVersionPage, error)
	RestoreProjectVersion(context.Context, RestoreProjectVersionRecord) (RestoreProjectVersionResult, error)
}

type ProjectVersions struct {
	repository ProjectVersionRepository
	identities IdentityGenerator
	clock      Clock
}

func NewProjectVersions(repository ProjectVersionRepository, identities IdentityGenerator, clock Clock) (*ProjectVersions, error) {
	if repository == nil || identities == nil || clock == nil {
		return nil, fmt.Errorf("project version dependencies are required")
	}
	return &ProjectVersions{repository: repository, identities: identities, clock: clock}, nil
}

func (versions *ProjectVersions) Create(
	ctx context.Context,
	projectID domain.ProjectID,
	input CreateProjectVersionInput,
) (CreateProjectVersionResult, error) {
	authority, err := projectVersionAuthority(ctx)
	name := strings.TrimSpace(input.Name)
	if err != nil || projectID.IsZero() || !validProjectVersionName(name) {
		return CreateProjectVersionResult{}, ErrProjectVersionInvalid
	}
	requestID, err := domain.ParseRequestID(input.RequestID.String())
	if err != nil {
		return CreateProjectVersionResult{}, ErrProjectVersionInvalid
	}
	canonical, digest, err := domain.CanonicalDigest("open-cut/project-version-request", ProjectVersionRequestSchema, struct {
		Command   string           `json:"command"`
		Creator   domain.ActorRef  `json:"creator"`
		Name      string           `json:"name"`
		ProjectID domain.ProjectID `json:"projectId"`
	}{Command: "create", Creator: authority.Actor, Name: name, ProjectID: projectID})
	if err != nil {
		return CreateProjectVersionResult{}, err
	}
	now := versions.clock.Now().UTC()
	ids, err := versions.newIDs(ctx, now, 2)
	if err != nil {
		return CreateProjectVersionResult{}, err
	}
	versionID, _ := domain.ParseProjectVersionID(ids[0])
	eventID, _ := domain.ParseActivityEventID(ids[1])
	return versions.repository.CreateProjectVersion(ctx, CreateProjectVersionRecord{
		ID: versionID, ProjectID: projectID, Creator: authority.Actor, RequestID: requestID,
		RequestDigest: digest, RequestCanonical: canonical, Name: name,
		ActivityEventID: eventID, CreatedAt: now,
	})
}

func (versions *ProjectVersions) List(
	ctx context.Context,
	projectID domain.ProjectID,
	before domain.ProjectVersionID,
	limit uint16,
) (ProjectVersionPage, error) {
	if _, err := projectVersionAuthority(ctx); err != nil || projectID.IsZero() || limit < 1 || limit > 50 {
		return ProjectVersionPage{}, ErrProjectVersionInvalid
	}
	return versions.repository.ListProjectVersions(ctx, projectID, before, limit)
}

func (versions *ProjectVersions) Restore(
	ctx context.Context,
	projectID domain.ProjectID,
	versionID domain.ProjectVersionID,
	input RestoreProjectVersionInput,
) (RestoreProjectVersionResult, error) {
	authority, err := projectVersionAuthority(ctx)
	if err != nil || projectID.IsZero() || versionID.IsZero() || input.ExpectedProjectRevision.Value() < 1 {
		return RestoreProjectVersionResult{}, ErrProjectVersionInvalid
	}
	requestID, err := domain.ParseRequestID(input.RequestID.String())
	if err != nil {
		return RestoreProjectVersionResult{}, ErrProjectVersionInvalid
	}
	canonical, digest, err := domain.CanonicalDigest("open-cut/project-version-request", ProjectVersionRequestSchema, struct {
		Command                 string                  `json:"command"`
		Creator                 domain.ActorRef         `json:"creator"`
		ExpectedProjectRevision domain.Revision         `json:"expectedProjectRevision"`
		ProjectID               domain.ProjectID        `json:"projectId"`
		VersionID               domain.ProjectVersionID `json:"versionId"`
	}{Command: "restore", Creator: authority.Actor, ExpectedProjectRevision: input.ExpectedProjectRevision,
		ProjectID: projectID, VersionID: versionID})
	if err != nil {
		return RestoreProjectVersionResult{}, err
	}
	now := versions.clock.Now().UTC()
	ids, err := versions.newIDs(ctx, now, 5)
	if err != nil {
		return RestoreProjectVersionResult{}, err
	}
	safetyID, _ := domain.ParseProjectVersionID(ids[0])
	proposalID, _ := domain.ParseProposalID(ids[1])
	applicationID, _ := domain.ParseProposalApplicationID(ids[2])
	transactionID, _ := domain.ParseTransactionID(ids[3])
	eventID, _ := domain.ParseActivityEventID(ids[4])
	return versions.repository.RestoreProjectVersion(ctx, RestoreProjectVersionRecord{
		ProjectID: projectID, VersionID: versionID, SafetyVersionID: safetyID,
		Creator: authority.Actor, RequestID: requestID, RequestDigest: digest, RequestCanonical: canonical,
		ExpectedProjectRevision: input.ExpectedProjectRevision, ProposalID: proposalID,
		ApplicationID: applicationID, TransactionID: transactionID, ActivityEventID: eventID, OccurredAt: now,
	})
}

func (versions *ProjectVersions) newIDs(ctx context.Context, at time.Time, count int) ([]string, error) {
	result := make([]string, count)
	for index := range result {
		value, err := versions.identities.NewID(ctx, at)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrIdentityGeneration, err)
		}
		result[index] = value
	}
	return result, nil
}

func projectVersionAuthority(ctx context.Context) (Authority, error) {
	authority, err := AuthorityFromContext(ctx)
	if err != nil || authority.Surface != AuthorityFirstPartyUI || authority.Actor.Kind != domain.ActorCreator {
		return Authority{}, ErrAuthorityScopeDenied
	}
	return authority, nil
}

func validProjectVersionName(value string) bool {
	return utf8.ValidString(value) && value != "" && utf8.RuneCountInString(value) <= 200
}
