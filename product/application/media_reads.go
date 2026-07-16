package application

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/PerishCode/open-cut/product/domain"
)

var ErrInvalidAssetCursor = errors.New("invalid asset page cursor")

type AssetView struct {
	ID                  domain.AssetID           `json:"id"`
	Revision            domain.Revision          `json:"revision"`
	ProjectID           domain.ProjectID         `json:"projectId"`
	DisplayName         string                   `json:"displayName"`
	ImportMode          domain.AssetImportMode   `json:"importMode" enum:"referenced,managed"`
	AcceptedFingerprint *domain.Digest           `json:"acceptedFingerprint,omitempty"`
	Tombstoned          bool                     `json:"tombstoned"`
	Availability        domain.AssetAvailability `json:"availability" enum:"identifying,online,changed,missing,managed,unreadable"`
	Fingerprint         *domain.Digest           `json:"fingerprint,omitempty"`
	Facts               *domain.MediaFacts       `json:"facts,omitempty"`
	Artifacts           []domain.ArtifactSummary `json:"artifacts" maxItems:"32" nullable:"false"`
	Jobs                []domain.MediaJobSummary `json:"jobs" maxItems:"32" nullable:"false"`
}

type AssetPage struct {
	Assets         []AssetView   `json:"assets" maxItems:"100" nullable:"false"`
	NextAfter      string        `json:"nextAfter,omitempty"`
	ActivityCursor domain.Cursor `json:"activityCursor"`
}

type AssetListQuery struct {
	ProjectID domain.ProjectID
	AfterID   string
	Limit     int
}

type AssetListResult struct {
	Assets         []domain.AssetDetail
	HasMore        bool
	ActivityCursor domain.Cursor
}

type AssetReadRepository interface {
	ListAssetDetails(context.Context, AssetListQuery) (AssetListResult, error)
	ReadAssetDetail(context.Context, domain.ProjectID, domain.AssetID) (domain.AssetDetail, domain.Cursor, error)
}

type AssetReads struct {
	repository AssetReadRepository
}

func NewAssetReads(repository AssetReadRepository) (*AssetReads, error) {
	if repository == nil {
		return nil, fmt.Errorf("asset read repository is required")
	}
	return &AssetReads{repository: repository}, nil
}

func (reads *AssetReads) List(
	ctx context.Context,
	projectID domain.ProjectID,
	after string,
	limit uint16,
) (AssetPage, error) {
	if _, err := AuthorityFromContext(ctx); err != nil {
		return AssetPage{}, err
	}
	pageLimit := boundedLimit(limit, 50, 100)
	if projectID.IsZero() || pageLimit == 0 {
		return AssetPage{}, ErrAssetInvalid
	}
	afterID, err := decodeAssetCursor(after)
	if err != nil {
		return AssetPage{}, err
	}
	result, err := reads.repository.ListAssetDetails(ctx, AssetListQuery{
		ProjectID: projectID, AfterID: afterID, Limit: pageLimit,
	})
	if err != nil {
		return AssetPage{}, err
	}
	page := AssetPage{Assets: make([]AssetView, 0, len(result.Assets)), ActivityCursor: result.ActivityCursor}
	for _, asset := range result.Assets {
		page.Assets = append(page.Assets, safeAssetView(asset))
	}
	if result.HasMore && len(page.Assets) > 0 {
		page.NextAfter = encodeAssetCursor(page.Assets[len(page.Assets)-1].ID.String())
	}
	return page, nil
}

func (reads *AssetReads) Inspect(
	ctx context.Context,
	projectID domain.ProjectID,
	assetID domain.AssetID,
) (AssetView, domain.Cursor, error) {
	if _, err := AuthorityFromContext(ctx); err != nil {
		return AssetView{}, 0, err
	}
	if projectID.IsZero() || assetID.IsZero() {
		return AssetView{}, 0, ErrAssetInvalid
	}
	detail, cursor, err := reads.repository.ReadAssetDetail(ctx, projectID, assetID)
	if err != nil {
		return AssetView{}, 0, err
	}
	return safeAssetView(detail), cursor, nil
}

func safeAssetView(detail domain.AssetDetail) AssetView {
	return AssetView{
		ID: detail.Asset.ID, Revision: detail.Asset.Revision, ProjectID: detail.Asset.ProjectID,
		DisplayName: detail.Asset.DisplayName, ImportMode: detail.Asset.ImportMode,
		AcceptedFingerprint: detail.Asset.AcceptedFingerprint, Tombstoned: detail.Asset.Tombstoned,
		Availability: detail.Availability, Fingerprint: detail.Fingerprint, Facts: safeMediaFacts(detail.Facts),
		Artifacts: nonNilSliceCopy(detail.Artifacts),
		Jobs:      safeMediaJobs(detail.Jobs),
	}
}

func safeMediaFacts(facts *domain.MediaFacts) *domain.MediaFacts {
	if facts == nil {
		return nil
	}
	view := *facts
	view.ContainerAliases = nonNilSliceCopy(facts.ContainerAliases)
	view.Streams = nonNilSliceCopy(facts.Streams)
	for index := range view.Streams {
		view.Streams[index].Descriptor.Dispositions = nonNilSliceCopy(
			facts.Streams[index].Descriptor.Dispositions,
		)
	}
	return &view
}

func safeMediaJobs(jobs []domain.MediaJobSummary) []domain.MediaJobSummary {
	view := nonNilSliceCopy(jobs)
	for index := range view {
		view[index].Prerequisites = nonNilSliceCopy(jobs[index].Prerequisites)
	}
	return view
}

func nonNilSliceCopy[T any](values []T) []T {
	return append(make([]T, 0, len(values)), values...)
}

func encodeAssetCursor(id string) string {
	return "asset.v1." + base64.RawURLEncoding.EncodeToString([]byte(id))
}

func decodeAssetCursor(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	const prefix = "asset.v1."
	if !strings.HasPrefix(value, prefix) {
		return "", ErrInvalidAssetCursor
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil {
		return "", ErrInvalidAssetCursor
	}
	id, err := domain.ParseAssetID(string(decoded))
	if err != nil {
		return "", ErrInvalidAssetCursor
	}
	return id.String(), nil
}
