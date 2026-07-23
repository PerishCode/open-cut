package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

var (
	ErrNoWork        = errors.New("no eligible durable work")
	ErrWorkLeaseLost = errors.New("work job attempt lease was lost")
)

type WorkExecutorRegistration struct {
	Kind    domain.WorkJobKind
	Version string
	Target  string
}

type WorkJobClaim struct {
	JobID           domain.WorkJobID
	AttemptID       domain.JobAttemptID
	Kind            domain.WorkJobKind
	ExecutorVersion string
	ExecutorTarget  string
	Generation      uint64
	LeaseOwner      string
	LeaseExpiresAt  time.Time
	Media           *MediaJobClaim
	SequencePreview *SequencePreviewJobClaim
	SequenceExport  *SequenceExportJobClaim
	SequenceFrames  *SequenceFrameJobClaim
	Resource        *ProductResourceJobClaim
}

type ClaimWorkJobInput struct {
	AttemptID     domain.JobAttemptID
	Executors     []WorkExecutorRegistration
	Resources     []ProductResourceRegistration
	LeaseOwner    string
	Now           time.Time
	LeaseDuration time.Duration
}

type WorkLeaseRepository interface {
	RecoverWorkJobs(
		context.Context,
		[]WorkExecutorRegistration,
		[]ProductResourceRegistration,
		time.Time,
	) error
	ClaimWorkJob(context.Context, ClaimWorkJobInput) (WorkJobClaim, error)
	RenewWorkJobLease(context.Context, WorkJobClaim, time.Time, time.Duration) error
}

type WorkJobExecutor interface {
	Registration() WorkExecutorRegistration
	Execute(context.Context, WorkJobClaim) error
}

type WorkSchedulerSettings struct {
	LeaseOwner    string
	LeaseDuration time.Duration
	PollInterval  time.Duration
	Resources     []ProductResourceRegistration
}

type WorkScheduler struct {
	repository WorkLeaseRepository
	executors  map[domain.WorkJobKind]WorkJobExecutor
	claims     []WorkExecutorRegistration
	identities IdentityGenerator
	clock      Clock
	settings   WorkSchedulerSettings
}

func NewWorkScheduler(
	repository WorkLeaseRepository,
	executors []WorkJobExecutor,
	identities IdentityGenerator,
	clock Clock,
	settings WorkSchedulerSettings,
) (*WorkScheduler, error) {
	if repository == nil || len(executors) == 0 || identities == nil || clock == nil ||
		settings.LeaseOwner == "" || len(settings.LeaseOwner) > 128 ||
		settings.LeaseDuration < 3*time.Second || settings.LeaseDuration > 10*time.Minute ||
		settings.PollInterval < 10*time.Millisecond || settings.PollInterval > time.Minute ||
		ValidateProductResourceRegistrations(settings.Resources) != nil {
		return nil, fmt.Errorf("work scheduler dependencies or settings are invalid")
	}
	registry := make(map[domain.WorkJobKind]WorkJobExecutor, len(executors))
	claims := make([]WorkExecutorRegistration, 0, len(executors))
	for _, executor := range executors {
		if executor == nil {
			return nil, fmt.Errorf("work scheduler executor is invalid")
		}
		registration := executor.Registration()
		if registration.Kind == "" || registration.Version == "" || len(registration.Version) > 1024 ||
			len(registration.Target) > 128 {
			return nil, fmt.Errorf("work scheduler executor registration is invalid")
		}
		if _, duplicate := registry[registration.Kind]; duplicate {
			return nil, fmt.Errorf("work scheduler repeats an executor kind")
		}
		registry[registration.Kind] = executor
		claims = append(claims, registration)
	}
	return &WorkScheduler{
		repository: repository, executors: registry, claims: claims,
		identities: identities, clock: clock, settings: WorkSchedulerSettings{
			LeaseOwner: settings.LeaseOwner, LeaseDuration: settings.LeaseDuration,
			PollInterval: settings.PollInterval,
			Resources:    append([]ProductResourceRegistration(nil), settings.Resources...),
		},
	}, nil
}

func (scheduler *WorkScheduler) Recover(ctx context.Context) error {
	return scheduler.repository.RecoverWorkJobs(
		ctx, append([]WorkExecutorRegistration(nil), scheduler.claims...),
		append([]ProductResourceRegistration(nil), scheduler.settings.Resources...), scheduler.clock.Now().UTC(),
	)
}

func (scheduler *WorkScheduler) Run(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			executed, err := scheduler.RunOne(ctx)
			if err != nil {
				return err
			}
			delay := scheduler.settings.PollInterval
			if executed {
				delay = 0
			}
			timer.Reset(delay)
		}
	}
}

func (scheduler *WorkScheduler) RunOne(ctx context.Context) (bool, error) {
	now := scheduler.clock.Now().UTC()
	attemptID, err := scheduler.newAttemptID(ctx, now)
	if err != nil {
		return false, fmt.Errorf("allocate work attempt: %w", err)
	}
	claim, err := scheduler.repository.ClaimWorkJob(ctx, ClaimWorkJobInput{
		AttemptID: attemptID, Executors: append([]WorkExecutorRegistration(nil), scheduler.claims...),
		Resources:  append([]ProductResourceRegistration(nil), scheduler.settings.Resources...),
		LeaseOwner: scheduler.settings.LeaseOwner,
		Now:        now, LeaseDuration: scheduler.settings.LeaseDuration,
	})
	if errors.Is(err, ErrNoWork) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim work job: %w", err)
	}
	executor, exists := scheduler.executors[claim.Kind]
	if !exists || executor.Registration().Version != claim.ExecutorVersion ||
		executor.Registration().Target != claim.ExecutorTarget {
		return true, fmt.Errorf(
			"resolve work job %s (%s): %w",
			claim.JobID.String(), claim.Kind, ErrWorkLeaseLost,
		)
	}
	executionContext, cancel := context.WithCancel(ctx)
	heartbeatDone := make(chan error, 1)
	go scheduler.heartbeat(executionContext, claim, heartbeatDone, cancel)
	executionErr := executor.Execute(executionContext, claim)
	cancel()
	heartbeatErr := <-heartbeatDone
	if heartbeatErr != nil {
		return true, fmt.Errorf(
			"heartbeat work job %s (%s): %w",
			claim.JobID.String(), claim.Kind, heartbeatErr,
		)
	}
	if executionErr != nil {
		return true, fmt.Errorf(
			"execute work job %s (%s): %w",
			claim.JobID.String(), claim.Kind, executionErr,
		)
	}
	return true, nil
}

func (scheduler *WorkScheduler) heartbeat(
	ctx context.Context,
	claim WorkJobClaim,
	done chan<- error,
	cancel context.CancelFunc,
) {
	interval := scheduler.settings.LeaseDuration / 3
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case <-ticker.C:
			if err := scheduler.repository.RenewWorkJobLease(
				ctx, claim, scheduler.clock.Now().UTC(), scheduler.settings.LeaseDuration,
			); err != nil {
				cancel()
				done <- err
				return
			}
		}
	}
}

func (scheduler *WorkScheduler) newAttemptID(
	ctx context.Context,
	at time.Time,
) (domain.JobAttemptID, error) {
	value, err := scheduler.identities.NewID(ctx, at)
	if err != nil {
		return domain.JobAttemptID{}, err
	}
	return domain.ParseJobAttemptID(value)
}

type mediaWorkExecutorAdapter struct {
	dispatcher   *mediaWorkDispatcher
	registration WorkExecutorRegistration
}

func NewMediaWorkExecutors(
	repository MediaWorkRepository,
	executors []MediaJobExecutor,
	identities IdentityGenerator,
	clock Clock,
) ([]WorkJobExecutor, error) {
	dispatcher, registrations, err := newMediaWorkDispatcher(repository, executors, identities, clock)
	if err != nil {
		return nil, err
	}
	result := make([]WorkJobExecutor, 0, len(registrations))
	for _, registration := range registrations {
		result = append(result, mediaWorkExecutorAdapter{
			dispatcher: dispatcher,
			registration: WorkExecutorRegistration{
				Kind: domain.WorkJobKind(registration.Kind), Version: registration.Version, Target: registration.Target,
			},
		})
	}
	return result, nil
}

func (adapter mediaWorkExecutorAdapter) Registration() WorkExecutorRegistration {
	return adapter.registration
}

func (adapter mediaWorkExecutorAdapter) Execute(ctx context.Context, claim WorkJobClaim) error {
	if claim.Media == nil || claim.Kind != adapter.registration.Kind ||
		claim.ExecutorVersion != adapter.registration.Version ||
		claim.ExecutorTarget != adapter.registration.Target {
		return ErrWorkLeaseLost
	}
	_, err := adapter.dispatcher.executeClaim(ctx, *claim.Media)
	return err
}
