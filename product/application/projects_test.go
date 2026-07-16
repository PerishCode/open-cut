package application

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestCreateProjectExpandsDefaultsAndConvergesRetries(t *testing.T) {
	repository := &memoryGenesisRepository{outcomes: make(map[string]storedGenesis)}
	identities := &sequenceIdentities{}
	projects, err := NewProjects(repository, identities, ClockFunc(func() time.Time { return applicationInstant }))
	if err != nil {
		t.Fatal(err)
	}
	requestID, _ := domain.ParseRequestID("create-project-001")
	input := CreateProjectInput{RequestID: requestID, Name: "Launch"}
	ctx := applicationCreatorContext(t)
	first, err := projects.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed || first.Project.Format != domain.DefaultSequenceFormat() ||
		first.ProjectActivityCursor.Value() != 1 || first.InstallationActivityCursor.Value() != 1 {
		t.Fatalf("first = %+v", first)
	}
	second, err := projects.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed || second.Project.Project.ID != first.Project.Project.ID || second.RequestDigest != first.RequestDigest {
		t.Fatalf("second = %+v first = %+v", second, first)
	}
	if identities.calls != 22 {
		t.Fatalf("identity calls = %d", identities.calls)
	}

	input.Name = "Different"
	if _, err := projects.Create(ctx, input); !errors.Is(err, ErrRequestIdentityReused) {
		t.Fatalf("request reuse error = %v", err)
	}
}

func TestCreateProjectRejectsInvalidGeneratedIdentity(t *testing.T) {
	projects, err := NewProjects(
		&memoryGenesisRepository{outcomes: make(map[string]storedGenesis)},
		IdentityGenerator(identityFunc(func(context.Context, time.Time) (string, error) { return "caller-id", nil })),
		ClockFunc(func() time.Time { return applicationInstant }),
	)
	if err != nil {
		t.Fatal(err)
	}
	requestID, _ := domain.ParseRequestID("create-project-001")
	_, err = projects.Create(applicationCreatorContext(t), CreateProjectInput{
		RequestID: requestID, Name: "Launch",
	})
	if !errors.Is(err, ErrIdentityGeneration) {
		t.Fatalf("identity error = %v", err)
	}
}

var applicationInstant = time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)

type identityFunc func(context.Context, time.Time) (string, error)

func (function identityFunc) NewID(ctx context.Context, at time.Time) (string, error) {
	return function(ctx, at)
}

type sequenceIdentities struct {
	calls int
}

func (identities *sequenceIdentities) NewID(context.Context, time.Time) (string, error) {
	identities.calls++
	return fmt.Sprintf("018f0000-0000-7000-8000-%012x", identities.calls), nil
}

type storedGenesis struct {
	digest             domain.Digest
	genesis            domain.ProjectGenesis
	projectCursor      domain.Cursor
	installationCursor domain.Cursor
}

type memoryGenesisRepository struct {
	outcomes map[string]storedGenesis
}

func (repository *memoryGenesisRepository) CreateGenesis(_ context.Context, record CreateProjectRecord) (CreateProjectOutcome, error) {
	key := string(record.Actor.Kind) + "\x00" + record.Actor.IDString() + "\x00" + record.Genesis.Record.RequestID.String()
	if existing, ok := repository.outcomes[key]; ok {
		if existing.digest != record.RequestDigest {
			return CreateProjectOutcome{}, ErrRequestIdentityReused
		}
		return CreateProjectOutcome{
			Genesis: existing.genesis, ProjectActivityCursor: existing.projectCursor,
			InstallationActivityCursor: existing.installationCursor, Replayed: true,
		}, nil
	}
	cursor, _ := domain.NewCursor(uint64(len(repository.outcomes) + 1))
	repository.outcomes[key] = storedGenesis{
		digest: record.RequestDigest, genesis: record.Genesis,
		projectCursor: cursor, installationCursor: cursor,
	}
	return CreateProjectOutcome{
		Genesis: record.Genesis, ProjectActivityCursor: cursor, InstallationActivityCursor: cursor,
	}, nil
}

func applicationCreatorActor() domain.ActorRef {
	creator, _ := domain.ParseCreatorID("018f0000-0000-7000-8000-0000000000ff")
	return domain.CreatorActor(creator)
}

func applicationCreatorContext(t *testing.T) context.Context {
	t.Helper()
	ctx, err := ContextWithAuthority(context.Background(), Authority{
		Surface: AuthorityFirstPartyUI, InstallationID: "installation-application-test",
		Actor: applicationCreatorActor(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}
