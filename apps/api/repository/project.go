package repository

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type memoryGenesis struct {
	digest             domain.Digest
	genesis            domain.ProjectGenesis
	projectCursor      domain.Cursor
	installationCursor domain.Cursor
}

type memoryRunRequest struct {
	command   string
	digest    domain.Digest
	projectID domain.ProjectID
	runID     domain.RunID
}

// MemoryProjects is a deterministic application-port adapter for controller
// and OpenAPI tests. Runtime composition always uses SQLiteProjects.
type MemoryProjects struct {
	mu                 sync.RWMutex
	requests           map[string]memoryGenesis
	projects           map[string]domain.ProjectGenesis
	installationHeads  map[string]domain.Cursor
	activity           map[string][]application.ActivityEvent
	creator            domain.CreatorID
	grants             map[string]application.CLIGrant
	grantKeys          map[string]string
	grantUpgrades      map[string]application.CLIGrantScopeUpgrade
	pendingUpgrades    map[string]string
	authorizationAudit []application.AuthorizationAudit
	invocationPolicy   application.InvocationPolicySettings
	runs               map[string]application.AgentRunDetail
	runRequests        map[string]memoryRunRequest
	commandReceipts    map[string][]application.CommandReceipt
}

func (repository *MemoryProjects) EnsureLocalCreator(
	_ context.Context,
	candidate domain.CreatorID,
	_ time.Time,
) (domain.CreatorID, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if candidate.IsZero() {
		return domain.CreatorID{}, domain.ErrInvalidDurableID
	}
	if repository.creator.IsZero() {
		repository.creator = candidate
	}
	return repository.creator, nil
}

func (repository *MemoryProjects) AppendAuthorizationAudit(
	_ context.Context,
	record application.AuthorizationAudit,
) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.authorizationAudit = append(repository.authorizationAudit, record)
	return nil
}

func (repository *MemoryProjects) AuthorizationAudits() []application.AuthorizationAudit {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	return append([]application.AuthorizationAudit(nil), repository.authorizationAudit...)
}

func NewMemoryProjects() *MemoryProjects {
	revision, _ := domain.NewRevision(1)
	return &MemoryProjects{
		requests: make(map[string]memoryGenesis), projects: make(map[string]domain.ProjectGenesis),
		installationHeads: make(map[string]domain.Cursor), activity: make(map[string][]application.ActivityEvent),
		grants: make(map[string]application.CLIGrant), grantKeys: make(map[string]string),
		grantUpgrades: make(map[string]application.CLIGrantScopeUpgrade), pendingUpgrades: make(map[string]string),
		runs: make(map[string]application.AgentRunDetail), runRequests: make(map[string]memoryRunRequest),
		commandReceipts: make(map[string][]application.CommandReceipt),
		invocationPolicy: application.InvocationPolicySettings{
			Revision: revision, Policy: application.DefaultInvocationPolicy(),
		},
	}
}

func (repository *MemoryProjects) CreateGenesis(
	_ context.Context,
	record application.CreateProjectRecord,
) (application.CreateProjectOutcome, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	key := string(record.Actor.Kind) + "\x00" + record.Actor.IDString() + "\x00" + record.Genesis.Record.RequestID.String()
	if existing, ok := repository.requests[key]; ok {
		if existing.digest != record.RequestDigest {
			return application.CreateProjectOutcome{}, application.ErrRequestIdentityReused
		}
		return application.CreateProjectOutcome{
			Genesis: existing.genesis, ProjectActivityCursor: existing.projectCursor,
			InstallationActivityCursor: existing.installationCursor, Replayed: true,
		}, nil
	}
	projectCursor, _ := domain.NewCursor(1)
	installationValue := repository.installationHeads[record.InstallationID].Value() + 1
	installationCursor, _ := domain.NewCursor(installationValue)
	repository.installationHeads[record.InstallationID] = installationCursor
	stored := memoryGenesis{
		digest: record.RequestDigest, genesis: record.Genesis,
		projectCursor: projectCursor, installationCursor: installationCursor,
	}
	repository.requests[key] = stored
	repository.projects[record.Genesis.Project.ID.String()] = record.Genesis
	repository.appendGenesisActivity(record, projectCursor, installationCursor)
	return application.CreateProjectOutcome{
		Genesis: record.Genesis, ProjectActivityCursor: projectCursor,
		InstallationActivityCursor: installationCursor,
	}, nil
}

func (repository *MemoryProjects) ListProjects(
	_ context.Context,
	query application.ProjectListQuery,
) (application.ProjectListPage, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	projects := make([]application.ProjectSummary, 0, len(repository.projects))
	for _, genesis := range repository.projects {
		project := genesis.Project
		if project.Status != query.Status || project.ID.String() <= query.AfterID {
			continue
		}
		document := project.NarrativeDocuments[0]
		sequence := project.Sequences[0]
		projects = append(projects, application.ProjectSummary{
			ID: project.ID, Revision: project.Revision, LifecycleRevision: project.LifecycleRevision,
			Name: project.Name, Status: project.Status, NarrativeDocumentID: document.ID, MainSequenceID: sequence.ID,
		})
	}
	sort.Slice(projects, func(left, right int) bool { return projects[left].ID.String() < projects[right].ID.String() })
	hasMore := len(projects) > query.Limit
	if hasMore {
		projects = projects[:query.Limit]
	}
	return application.ProjectListPage{
		Projects: projects, HasMore: hasMore, ActivityCursor: repository.installationHeads[query.ScopeID],
	}, nil
}

func (repository *MemoryProjects) ShowProject(
	_ context.Context,
	id domain.ProjectID,
) (application.ProjectOverview, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	genesis, ok := repository.projects[id.String()]
	if !ok {
		return application.ProjectOverview{}, application.ErrProjectNotFound
	}
	project := genesis.Project
	document := project.NarrativeDocuments[0]
	sequence := project.Sequences[0]
	tracks := make([]application.TrackSummary, 0, len(sequence.Tracks))
	for _, track := range sequence.Tracks {
		tracks = append(tracks, application.TrackSummary{ID: track.ID, Revision: track.Revision, Type: track.Type, Label: track.Label})
	}
	cursor, _ := domain.NewCursor(1)
	return application.ProjectOverview{
		Project: application.ProjectSummary{
			ID: project.ID, Revision: project.Revision, LifecycleRevision: project.LifecycleRevision,
			Name: project.Name, Status: project.Status, NarrativeDocumentID: document.ID, MainSequenceID: sequence.ID,
		},
		NarrativeDocumentRevision: document.Revision, NarrativeRootNodeID: document.RootNodeID,
		MainSequenceRevision: sequence.Revision,
		Format:               sequence.Format, Tracks: tracks, ActivityCursor: cursor,
	}, nil
}

func (repository *MemoryProjects) ListActivity(
	_ context.Context,
	query application.ActivityQuery,
) (application.ActivityPage, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	var head domain.Cursor
	if query.Scope.Kind == application.ActivityScopeInstallation {
		head = repository.installationHeads[query.Scope.ID]
	} else if query.Scope.Kind == application.ActivityScopeProject {
		if _, ok := repository.projects[query.Scope.ID]; ok {
			stored := repository.activity[activityScopeKey(query.Scope)]
			if len(stored) > 0 {
				head = stored[len(stored)-1].Cursor
			}
		}
	} else {
		return application.ActivityPage{}, application.ErrInvalidActivityScope
	}
	if query.After.Value() > head.Value() {
		return application.ActivityPage{}, application.ErrInvalidActivityScope
	}
	stored := repository.activity[activityScopeKey(query.Scope)]
	events := make([]application.ActivityEvent, 0, min(len(stored), query.Limit+1))
	for _, event := range stored {
		if event.Cursor.Value() > query.After.Value() {
			events = append(events, event)
		}
		if len(events) > query.Limit {
			break
		}
	}
	hasMore := len(events) > query.Limit
	if hasMore {
		events = events[:query.Limit]
	}
	cursor := head
	if hasMore && len(events) > 0 {
		cursor = events[len(events)-1].Cursor
	}
	return application.ActivityPage{Events: events, Cursor: cursor, HasMore: hasMore}, nil
}

func (repository *MemoryProjects) appendGenesisActivity(
	record application.CreateProjectRecord,
	projectCursor domain.Cursor,
	installationCursor domain.Cursor,
) {
	genesis := record.Genesis
	project := genesis.Project
	actor := &application.ActivityActor{Kind: record.Actor.Kind, ID: record.Actor.IDString()}
	projectID := project.ID
	projectRevision := project.Revision
	projectScope := application.ActivityScope{Kind: application.ActivityScopeProject, ID: project.ID.String()}
	projectEvent := application.ActivityEvent{
		Schema: application.ActivitySchema, EventID: genesis.Record.ActivityEventID,
		Scope: projectScope, Cursor: projectCursor, Kind: "project.created", OccurredAt: genesis.Record.CreatedAt,
		Actor: actor, ProjectID: &projectID, ProjectRevision: &projectRevision,
		Outcome:     &application.ActivityOutcomeRef{Kind: "transaction", ID: genesis.Record.TransactionID.String()},
		SummaryCode: "project-created", ChangedEntityRefs: []application.ChangedEntityRef{},
	}
	installationScope := application.ActivityScope{
		Kind: application.ActivityScopeInstallation, ID: record.InstallationID,
	}
	installationEvent := application.ActivityEvent{
		Schema: application.ActivitySchema, EventID: record.ActivityEventID,
		Scope: installationScope, Cursor: installationCursor, Kind: "workspace.project-created",
		OccurredAt: genesis.Record.CreatedAt, Actor: actor, ProjectID: &projectID, ProjectRevision: &projectRevision,
		Outcome:     &application.ActivityOutcomeRef{Kind: "project", ID: project.ID.String()},
		SummaryCode: "project-created", ChangedEntityRefs: []application.ChangedEntityRef{},
	}
	repository.activity[activityScopeKey(projectScope)] = append(
		repository.activity[activityScopeKey(projectScope)], projectEvent,
	)
	repository.activity[activityScopeKey(installationScope)] = append(
		repository.activity[activityScopeKey(installationScope)], installationEvent,
	)
}

func activityScopeKey(scope application.ActivityScope) string {
	return string(scope.Kind) + "\x00" + scope.ID
}
