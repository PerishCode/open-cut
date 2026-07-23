package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

const (
	SequenceExportDeliveryLeaseSchema = "open-cut/sequence-export-delivery-lease/v1"
	SequenceExportDeliveryLeaseTTL    = 2 * time.Minute
	sequenceExportDeliveryLeaseLimit  = 128
)

var (
	ErrSequenceExportDeliveryInvalid = errors.New("sequence export delivery lease is invalid")
	ErrSequenceExportDeliveryExpired = errors.New("sequence export delivery lease expired")
)

type SequenceExportDeliveryLease struct {
	Schema        string            `json:"schema" enum:"open-cut/sequence-export-delivery-lease/v1"`
	ArtifactID    domain.ArtifactID `json:"artifactId" format:"uuid"`
	MimeType      string            `json:"mimeType" enum:"video/webm"`
	ByteLength    domain.UInt64     `json:"byteLength" format:"uint64-decimal"`
	ContentSHA256 domain.Digest     `json:"contentSha256"`
	ContentURL    string            `json:"contentUrl"`
	ExpiresAt     time.Time         `json:"expiresAt" format:"date-time"`
}

type sequenceExportDeliveryOpener interface {
	InspectSequenceExportDelivery(
		context.Context,
		domain.ProjectID,
		domain.ArtifactID,
		time.Time,
	) (application.SequenceExportArtifactFile, error)
	OpenSequenceExportDelivery(
		context.Context,
		domain.ProjectID,
		domain.ArtifactID,
		time.Time,
	) (*os.File, application.SequenceExportArtifactFile, error)
}

type sequenceExportDeliveryLeaseRecord struct {
	binding    uiLeaseBinding
	projectID  domain.ProjectID
	artifactID domain.ArtifactID
	media      application.SequenceExportArtifactFile
	expiresAt  time.Time
}

type SequenceExportDeliveryService struct {
	mu     sync.Mutex
	opener sequenceExportDeliveryOpener
	clock  application.Clock
	random io.Reader
	leases map[string]sequenceExportDeliveryLeaseRecord
}

func NewSequenceExportDeliveryService(
	opener sequenceExportDeliveryOpener,
	clock application.Clock,
	random io.Reader,
) (*SequenceExportDeliveryService, error) {
	if opener == nil || clock == nil || random == nil {
		return nil, fmt.Errorf("sequence export delivery dependencies are required")
	}
	return &SequenceExportDeliveryService{
		opener: opener, clock: clock, random: random,
		leases: make(map[string]sequenceExportDeliveryLeaseRecord),
	}, nil
}

func (service *SequenceExportDeliveryService) Create(
	ctx context.Context,
	projectID domain.ProjectID,
	artifactID domain.ArtifactID,
) (SequenceExportDeliveryLease, error) {
	if err := requireCreatorExportDeliveryAuthority(ctx); err != nil {
		return SequenceExportDeliveryLease{}, err
	}
	binding, err := uiSessionBindingFromContext(ctx)
	if err != nil {
		return SequenceExportDeliveryLease{}, err
	}
	now := service.clock.Now().UTC()
	media, err := service.opener.InspectSequenceExportDelivery(ctx, projectID, artifactID, now)
	if err != nil {
		return SequenceExportDeliveryLease{}, err
	}
	if media.Path != "export.webm" || media.MimeType != "video/webm" || media.ByteSize.Value() == 0 {
		return SequenceExportDeliveryLease{}, ErrSequenceExportDeliveryInvalid
	}
	expiresAt := now.Add(SequenceExportDeliveryLeaseTTL)
	if binding.expiresAt.Before(expiresAt) {
		expiresAt = binding.expiresAt
	}
	if !now.Before(expiresAt) {
		return SequenceExportDeliveryLease{}, ErrSequenceExportDeliveryExpired
	}
	token, err := randomToken(service.random, "oc_export_")
	if err != nil {
		return SequenceExportDeliveryLease{}, err
	}
	record := sequenceExportDeliveryLeaseRecord{
		binding:   binding.leaseBinding(),
		projectID: projectID, artifactID: artifactID, media: media, expiresAt: expiresAt,
	}
	service.mu.Lock()
	service.cleanupLocked(now)
	if len(service.leases) >= sequenceExportDeliveryLeaseLimit {
		service.mu.Unlock()
		return SequenceExportDeliveryLease{}, ErrUIRateLimited
	}
	service.leases[tokenHash(token)] = record
	service.mu.Unlock()
	return SequenceExportDeliveryLease{
		Schema: SequenceExportDeliveryLeaseSchema, ArtifactID: artifactID,
		MimeType: media.MimeType, ByteLength: media.ByteSize, ContentSHA256: media.SHA256,
		ContentURL: "/v1/internal/platform/export-content/" + token, ExpiresAt: expiresAt,
	}, nil
}

func (service *SequenceExportDeliveryService) ServeContent(
	ctx context.Context,
	response http.ResponseWriter,
	token string,
) error {
	if err := requireCreatorExportDeliveryAuthority(ctx); err != nil {
		return err
	}
	binding, err := uiSessionBindingFromContext(ctx)
	if err != nil {
		return err
	}
	now := service.clock.Now().UTC()
	key := tokenHash(token)
	service.mu.Lock()
	service.cleanupLocked(now)
	record, exists := service.leases[key]
	if exists {
		delete(service.leases, key)
	}
	service.mu.Unlock()
	if !exists || !record.binding.matches(binding) {
		return ErrSequenceExportDeliveryInvalid
	}
	if !now.Before(record.expiresAt) {
		return ErrSequenceExportDeliveryExpired
	}
	file, media, err := service.opener.OpenSequenceExportDelivery(
		ctx, record.projectID, record.artifactID, now,
	)
	if err != nil {
		return err
	}
	defer file.Close()
	if media != record.media {
		return ErrSequenceExportDeliveryInvalid
	}
	response.Header().Set("Cache-Control", "private, no-store, max-age=0")
	response.Header().Set("Content-Length", strconv.FormatUint(media.ByteSize.Value(), 10))
	response.Header().Set("Content-Type", media.MimeType)
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("X-Open-Cut-Content-SHA256", media.SHA256.String())
	response.WriteHeader(http.StatusOK)
	written, err := io.Copy(response, io.LimitReader(file, int64(media.ByteSize.Value())))
	if err != nil {
		return err
	}
	if uint64(written) != media.ByteSize.Value() {
		return ErrSequenceExportDeliveryInvalid
	}
	return nil
}

func (service *SequenceExportDeliveryService) cleanupLocked(now time.Time) {
	for key, record := range service.leases {
		if !now.Before(record.expiresAt) {
			delete(service.leases, key)
		}
	}
}

func requireCreatorExportDeliveryAuthority(ctx context.Context) error {
	authority, err := application.AuthorityFromContext(ctx)
	if err != nil {
		return err
	}
	if authority.Surface != application.AuthorityFirstPartyUI || authority.Actor.Kind != domain.ActorCreator {
		return application.ErrAuthorityScopeDenied
	}
	return nil
}
