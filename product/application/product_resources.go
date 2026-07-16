package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	ProductResourceCatalogEntrySchema = "open-cut/product-resource-catalog-entry/v1"
	ProductResourceSnapshotSchema     = "open-cut/product-resource-snapshot/v1"
	ProductResourceAcquireSchema      = "open-cut/product-resource-acquire/v1"
	ProductResourceDownloaderV1       = "product-resource-downloader/v1"
	MaximumProductResourceBytes       = uint64(16) << 30
	WorkJobResourceAcquire            = domain.WorkJobKind("resource-acquire")
)

var (
	ErrProductResourceInvalid   = errors.New("product resource request is invalid")
	ErrProductResourceNotFound  = errors.New("product resource is not declared by the active payload")
	ErrProductResourceLeaseLost = errors.New("product resource job attempt lease was lost")
)

type ProductResourceCatalogEntry struct {
	Name        string
	Kind        domain.ProductResourceKind
	Version     string
	Profile     string
	Origin      string
	ByteSize    domain.UInt64
	SHA256      domain.Digest
	Retention   domain.ProductResourceRetention
	EntryDigest domain.Digest
	Canonical   []byte
}

type productResourceCatalogCanonical struct {
	Name      string                          `json:"name"`
	Kind      domain.ProductResourceKind      `json:"kind"`
	Version   string                          `json:"version"`
	Profile   string                          `json:"profile"`
	Origin    string                          `json:"origin"`
	ByteSize  domain.UInt64                   `json:"byteSize"`
	SHA256    domain.Digest                   `json:"sha256"`
	Retention domain.ProductResourceRetention `json:"retention"`
}

func NewProductResourceCatalogEntry(
	name string,
	kind domain.ProductResourceKind,
	version, profile, origin string,
	byteSize domain.UInt64,
	sha256 domain.Digest,
	retention domain.ProductResourceRetention,
) (ProductResourceCatalogEntry, error) {
	entry := ProductResourceCatalogEntry{
		Name: name, Kind: kind, Version: version, Profile: profile, Origin: origin,
		ByteSize: byteSize, SHA256: sha256, Retention: retention,
	}
	canonical, digest, err := CanonicalProductResourceCatalogEntry(entry)
	if err != nil {
		return ProductResourceCatalogEntry{}, err
	}
	entry.EntryDigest = digest
	entry.Canonical = canonical
	return entry, nil
}

func CanonicalProductResourceCatalogEntry(
	entry ProductResourceCatalogEntry,
) ([]byte, domain.Digest, error) {
	parsed, err := url.Parse(entry.Origin)
	if !domain.ValidProductResourceName(entry.Name) || entry.Kind != domain.ProductResourceTranscriptionModel ||
		entry.Version == "" || len(entry.Version) > 128 || entry.Profile == "" || len(entry.Profile) > 128 ||
		err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.Fragment != "" || entry.ByteSize.Value() == 0 ||
		entry.ByteSize.Value() > MaximumProductResourceBytes ||
		entry.Retention != domain.ProductResourceRetentionOffline {
		return nil, "", ErrProductResourceInvalid
	}
	if _, err := domain.ParseDigest(entry.SHA256.String()); err != nil {
		return nil, "", ErrProductResourceInvalid
	}
	return domain.CanonicalDigest(
		"open-cut/product-resource-catalog-entry", ProductResourceCatalogEntrySchema,
		productResourceCatalogCanonical{
			Name: entry.Name, Kind: entry.Kind, Version: entry.Version, Profile: entry.Profile,
			Origin: entry.Origin, ByteSize: entry.ByteSize, SHA256: entry.SHA256, Retention: entry.Retention,
		},
	)
}

func ValidateProductResourceCatalog(entries []ProductResourceCatalogEntry) error {
	if len(entries) > 128 {
		return ErrProductResourceInvalid
	}
	previous := ""
	for _, entry := range entries {
		canonical, digest, err := CanonicalProductResourceCatalogEntry(entry)
		if err != nil || entry.Name <= previous || digest != entry.EntryDigest ||
			!bytes.Equal(canonical, entry.Canonical) {
			return ErrProductResourceInvalid
		}
		previous = entry.Name
	}
	return nil
}

type ProductResourceState string

const (
	ProductResourceNotAcquired ProductResourceState = "not-acquired"
	ProductResourceQueued      ProductResourceState = "queued"
	ProductResourceAcquiring   ProductResourceState = "acquiring"
	ProductResourceReady       ProductResourceState = "ready"
	ProductResourceFailed      ProductResourceState = "failed"
	ProductResourceCancelled   ProductResourceState = "cancelled"
)

type ProductResourceView struct {
	Name                string                     `json:"name" pattern:"^[a-z][a-z0-9.-]{0,127}$"`
	Kind                domain.ProductResourceKind `json:"kind" enum:"transcription-model"`
	Version             string                     `json:"version" minLength:"1" maxLength:"128"`
	Profile             string                     `json:"profile" minLength:"1" maxLength:"128"`
	ByteSize            domain.UInt64              `json:"byteSize" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	State               ProductResourceState       `json:"state" enum:"not-acquired,queued,acquiring,ready,failed,cancelled"`
	ResourceID          *domain.ResourceID         `json:"resourceId,omitempty"`
	JobID               *domain.WorkJobID          `json:"jobId,omitempty"`
	ProgressBasisPoints uint16                     `json:"progressBasisPoints" minimum:"0" maximum:"10000"`
	FailureCode         string                     `json:"failureCode,omitempty" maxLength:"64"`
	UpdatedAt           *time.Time                 `json:"updatedAt,omitempty"`
}

type ProductResourceSnapshot struct {
	Schema    string                `json:"schema" enum:"open-cut/product-resource-snapshot/v1"`
	Resources []ProductResourceView `json:"resources" nullable:"false" maxItems:"128"`
}

type AcquireProductResourceInput struct {
	RequestID domain.RequestID `json:"requestId" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
}

type AcquireProductResourceResult struct {
	Resource       ProductResourceView `json:"resource"`
	ActivityCursor domain.Cursor       `json:"activityCursor" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	Replayed       bool                `json:"replayed"`
}

type RequestProductResourceRecord struct {
	InstallationID   string
	Actor            domain.ActorRef
	RequestID        domain.RequestID
	RequestDigest    domain.Digest
	RequestCanonical []byte
	Entry            ProductResourceCatalogEntry
	JobID            domain.WorkJobID
	ActivityEventID  domain.ActivityEventID
	RequestedAt      time.Time
}

type RequestProductResourceOutcome struct {
	View           ProductResourceView
	ActivityCursor domain.Cursor
	Replayed       bool
}

type ProductResourceRepository interface {
	ListProductResources(context.Context, string, []ProductResourceCatalogEntry) ([]ProductResourceView, error)
	RequestProductResource(context.Context, RequestProductResourceRecord) (RequestProductResourceOutcome, error)
}

type ProductResources struct {
	repository ProductResourceRepository
	entries    []ProductResourceCatalogEntry
	identities IdentityGenerator
	clock      Clock
}

func NewProductResources(
	repository ProductResourceRepository,
	entries []ProductResourceCatalogEntry,
	identities IdentityGenerator,
	clock Clock,
) (*ProductResources, error) {
	entries = append([]ProductResourceCatalogEntry(nil), entries...)
	if repository == nil || identities == nil || clock == nil || ValidateProductResourceCatalog(entries) != nil {
		return nil, fmt.Errorf("product resource dependencies are invalid")
	}
	return &ProductResources{repository: repository, entries: entries, identities: identities, clock: clock}, nil
}

func (resources *ProductResources) RuntimeRegistrations() []ProductResourceRegistration {
	result := make([]ProductResourceRegistration, len(resources.entries))
	for index, entry := range resources.entries {
		result[index] = ProductResourceRegistration{
			Name: entry.Name, Profile: entry.Profile, EntryDigest: entry.EntryDigest,
		}
	}
	return result
}

func (resources *ProductResources) List(ctx context.Context) (ProductResourceSnapshot, error) {
	authority, err := productResourceAuthority(ctx)
	if err != nil {
		return ProductResourceSnapshot{}, err
	}
	views, err := resources.repository.ListProductResources(ctx, authority.InstallationID, resources.entries)
	if err != nil {
		return ProductResourceSnapshot{}, err
	}
	return ProductResourceSnapshot{Schema: ProductResourceSnapshotSchema, Resources: views}, nil
}

func (resources *ProductResources) Acquire(
	ctx context.Context,
	name string,
	input AcquireProductResourceInput,
) (AcquireProductResourceResult, error) {
	authority, err := productResourceAuthority(ctx)
	if err != nil {
		return AcquireProductResourceResult{}, err
	}
	if _, err := domain.ParseRequestID(input.RequestID.String()); err != nil {
		return AcquireProductResourceResult{}, ErrProductResourceInvalid
	}
	index, found := slices.BinarySearchFunc(resources.entries, name, func(entry ProductResourceCatalogEntry, name string) int {
		if entry.Name < name {
			return -1
		}
		if entry.Name > name {
			return 1
		}
		return 0
	})
	if !found {
		return AcquireProductResourceResult{}, ErrProductResourceNotFound
	}
	entry := resources.entries[index]
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/product-resource-acquire", ProductResourceAcquireSchema,
		struct {
			Name        string        `json:"name"`
			EntryDigest domain.Digest `json:"entryDigest"`
		}{Name: entry.Name, EntryDigest: entry.EntryDigest},
	)
	if err != nil {
		return AcquireProductResourceResult{}, err
	}
	now := resources.clock.Now().UTC()
	jobID, eventID, err := resources.newRequestIDs(ctx, now)
	if err != nil {
		return AcquireProductResourceResult{}, err
	}
	outcome, err := resources.repository.RequestProductResource(ctx, RequestProductResourceRecord{
		InstallationID: authority.InstallationID, Actor: authority.Actor, RequestID: input.RequestID,
		RequestDigest: digest, RequestCanonical: canonical, Entry: entry,
		JobID: jobID, ActivityEventID: eventID, RequestedAt: now,
	})
	if err != nil {
		return AcquireProductResourceResult{}, err
	}
	return AcquireProductResourceResult{
		Resource: outcome.View, ActivityCursor: outcome.ActivityCursor, Replayed: outcome.Replayed,
	}, nil
}

func (resources *ProductResources) newRequestIDs(
	ctx context.Context,
	at time.Time,
) (domain.WorkJobID, domain.ActivityEventID, error) {
	jobValue, err := resources.identities.NewID(ctx, at)
	if err != nil {
		return domain.WorkJobID{}, domain.ActivityEventID{}, err
	}
	jobID, err := domain.ParseWorkJobID(jobValue)
	if err != nil {
		return domain.WorkJobID{}, domain.ActivityEventID{}, err
	}
	eventValue, err := resources.identities.NewID(ctx, at)
	if err != nil {
		return domain.WorkJobID{}, domain.ActivityEventID{}, err
	}
	eventID, err := domain.ParseActivityEventID(eventValue)
	return jobID, eventID, err
}

func productResourceAuthority(ctx context.Context) (Authority, error) {
	authority, err := AuthorityFromContext(ctx)
	if err != nil {
		return Authority{}, err
	}
	if authority.Surface != AuthorityFirstPartyUI || authority.Actor.Kind != domain.ActorCreator {
		return Authority{}, ErrAuthorityScopeDenied
	}
	return authority, nil
}

type ProductResourceRegistration struct {
	Name        string
	Profile     string
	EntryDigest domain.Digest
}

func ValidateProductResourceRegistrations(registrations []ProductResourceRegistration) error {
	if len(registrations) > 128 {
		return ErrProductResourceInvalid
	}
	previous := ""
	for _, registration := range registrations {
		if !domain.ValidProductResourceName(registration.Name) || registration.Name <= previous ||
			registration.Profile == "" || len(registration.Profile) > 128 {
			return ErrProductResourceInvalid
		}
		if _, err := domain.ParseDigest(registration.EntryDigest.String()); err != nil {
			return ErrProductResourceInvalid
		}
		previous = registration.Name
	}
	return nil
}

type ProductResourceJobClaim struct {
	InstallationID string
	Entry          ProductResourceCatalogEntry
}

type PreparedProductResource interface {
	Open() (io.ReadCloser, error)
	Release() error
}

type ProductResourceDownload struct {
	ByteSize  domain.UInt64
	SHA256    domain.Digest
	Workspace PreparedProductResource
}

type ProductResourceDownloader interface {
	Version() string
	Download(context.Context, ProductResourceJobClaim) (ProductResourceDownload, error)
}

type CompleteProductResourceInput struct {
	Claim       WorkJobClaim
	ResourceID  domain.ResourceID
	Download    ProductResourceDownload
	EventID     domain.ActivityEventID
	CompletedAt time.Time
}

type FailProductResourceInput struct {
	Claim    WorkJobClaim
	Code     string
	EventID  domain.ActivityEventID
	FailedAt time.Time
}

type ProductResourceWorkRepository interface {
	CompleteProductResource(context.Context, CompleteProductResourceInput) error
	FailProductResource(context.Context, FailProductResourceInput) error
}

type productResourceWorkExecutor struct {
	repository ProductResourceWorkRepository
	downloader ProductResourceDownloader
	identities IdentityGenerator
	clock      Clock
}

func NewProductResourceWorkExecutor(
	repository ProductResourceWorkRepository,
	downloader ProductResourceDownloader,
	identities IdentityGenerator,
	clock Clock,
) (WorkJobExecutor, error) {
	if repository == nil || downloader == nil || identities == nil || clock == nil ||
		downloader.Version() == "" || len(downloader.Version()) > 256 {
		return nil, fmt.Errorf("product resource executor dependencies are invalid")
	}
	return &productResourceWorkExecutor{
		repository: repository, downloader: downloader, identities: identities, clock: clock,
	}, nil
}

func (executor *productResourceWorkExecutor) Registration() WorkExecutorRegistration {
	return WorkExecutorRegistration{Kind: WorkJobResourceAcquire, Version: executor.downloader.Version()}
}

func (executor *productResourceWorkExecutor) Execute(ctx context.Context, claim WorkJobClaim) error {
	if claim.Resource == nil || claim.Kind != WorkJobResourceAcquire ||
		claim.ExecutorVersion != executor.downloader.Version() {
		return ErrWorkLeaseLost
	}
	download, downloadErr := executor.downloader.Download(ctx, *claim.Resource)
	if download.Workspace != nil {
		defer download.Workspace.Release()
	}
	now := executor.clock.Now().UTC()
	eventID, err := executor.newEventID(ctx, now)
	if err != nil {
		return err
	}
	if downloadErr != nil {
		return executor.repository.FailProductResource(ctx, FailProductResourceInput{
			Claim: claim, Code: classifyProductResourceDownloadError(downloadErr), EventID: eventID, FailedAt: now,
		})
	}
	resourceID, err := executor.newResourceID(ctx, now)
	if err != nil {
		return err
	}
	return executor.repository.CompleteProductResource(ctx, CompleteProductResourceInput{
		Claim: claim, ResourceID: resourceID, Download: download, EventID: eventID, CompletedAt: now,
	})
}

func (executor *productResourceWorkExecutor) newEventID(
	ctx context.Context,
	at time.Time,
) (domain.ActivityEventID, error) {
	value, err := executor.identities.NewID(ctx, at)
	if err != nil {
		return domain.ActivityEventID{}, err
	}
	return domain.ParseActivityEventID(value)
}

func (executor *productResourceWorkExecutor) newResourceID(
	ctx context.Context,
	at time.Time,
) (domain.ResourceID, error) {
	value, err := executor.identities.NewID(ctx, at)
	if err != nil {
		return domain.ResourceID{}, err
	}
	return domain.ParseResourceID(value)
}

type ProductResourceDownloadError struct {
	Code  string
	Cause error
}

func (failure ProductResourceDownloadError) Error() string {
	if failure.Cause == nil {
		return failure.Code
	}
	return failure.Code + ": " + failure.Cause.Error()
}

func (failure ProductResourceDownloadError) Unwrap() error { return failure.Cause }

func NewProductResourceDownloadError(code string, cause error) error {
	if code == "" || len(code) > 64 {
		return ErrProductResourceInvalid
	}
	return ProductResourceDownloadError{Code: code, Cause: cause}
}

func classifyProductResourceDownloadError(err error) string {
	var failure ProductResourceDownloadError
	if errors.As(err, &failure) && failure.Code != "" && len(failure.Code) <= 64 {
		return failure.Code
	}
	return "resource-download-failed"
}
