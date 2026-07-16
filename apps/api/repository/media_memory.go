package repository

import (
	"context"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

// Media methods on MemoryProjects exist only for controller/OpenAPI fixtures.
// Runtime SourceGrant custody, journals, projections, and jobs always use SQLite.
func (repository *MemoryProjects) RegisterSourceGrant(
	context.Context,
	application.RegisterSourceGrantRecord,
) (application.SourceGrantResult, error) {
	return application.SourceGrantResult{}, application.ErrSourceGrantInvalid
}

func (repository *MemoryProjects) ReadSourceGrant(
	context.Context,
	string,
	domain.SourceGrantID,
) (domain.SourceGrantSummary, error) {
	return domain.SourceGrantSummary{}, application.ErrSourceGrantNotFound
}

func (repository *MemoryProjects) RegisterAsset(
	context.Context,
	application.RegisterAssetRecord,
) (application.AssetRegisterResult, error) {
	return application.AssetRegisterResult{}, application.ErrAssetInvalid
}

func (repository *MemoryProjects) ListAssetDetails(
	context.Context,
	application.AssetListQuery,
) (application.AssetListResult, error) {
	return application.AssetListResult{}, application.ErrProjectNotFound
}

func (repository *MemoryProjects) ReadAssetDetail(
	context.Context,
	domain.ProjectID,
	domain.AssetID,
) (domain.AssetDetail, domain.Cursor, error) {
	return domain.AssetDetail{}, 0, application.ErrAssetNotFound
}

func (repository *MemoryProjects) RequestMediaFrameSet(
	context.Context,
	application.RequestMediaFrameSetRecord,
) (application.MediaFrameSetRequestResult, error) {
	return application.MediaFrameSetRequestResult{}, application.ErrAssetInvalid
}

func (repository *MemoryProjects) MaterializeMediaFrameLeases(
	context.Context,
	application.MaterializeMediaFrameLeasesRecord,
) ([]application.FrameResourceLease, error) {
	return nil, application.ErrAssetInvalid
}

func (repository *MemoryProjects) RecoverMediaJobs(
	context.Context,
	[]application.MediaExecutorRegistration,
	time.Time,
) error {
	return nil
}

func (repository *MemoryProjects) ClaimMediaJob(
	context.Context,
	application.ClaimMediaJobInput,
) (application.MediaJobClaim, error) {
	return application.MediaJobClaim{}, application.ErrNoMediaWork
}

func (repository *MemoryProjects) RenewMediaJobLease(
	context.Context,
	application.MediaJobClaim,
	time.Time,
	time.Duration,
) error {
	return application.ErrMediaLeaseLost
}

func (repository *MemoryProjects) CompleteMediaIdentification(
	context.Context,
	application.CompleteMediaIdentification,
) error {
	return application.ErrMediaLeaseLost
}

func (repository *MemoryProjects) CompleteMediaProbe(
	context.Context,
	application.CompleteMediaProbe,
) error {
	return application.ErrMediaLeaseLost
}

func (repository *MemoryProjects) CompleteMediaFrameSet(
	context.Context,
	application.CompleteMediaFrameSet,
) error {
	return application.ErrMediaLeaseLost
}

func (repository *MemoryProjects) CompleteMediaProxy(
	context.Context,
	application.CompleteMediaProxy,
) error {
	return application.ErrMediaLeaseLost
}

func (repository *MemoryProjects) CompleteMediaRenderInput(
	context.Context,
	application.CompleteMediaRenderInput,
) error {
	return application.ErrMediaLeaseLost
}

func (repository *MemoryProjects) CompleteMediaTranscript(
	context.Context,
	application.CompleteMediaTranscript,
) error {
	return application.ErrMediaLeaseLost
}

func (repository *MemoryProjects) CompleteMediaTranscriptNoAudio(
	context.Context,
	application.CompleteMediaTranscriptNoAudio,
) error {
	return application.ErrMediaLeaseLost
}

func (repository *MemoryProjects) ReblockMediaTranscriptResource(
	context.Context,
	application.ReblockMediaTranscriptResource,
) error {
	return application.ErrMediaLeaseLost
}

func (repository *MemoryProjects) FailMediaJob(context.Context, application.FailMediaJobInput) error {
	return application.ErrMediaLeaseLost
}

func (repository *MemoryProjects) ReadAssetSourceMaterial(
	context.Context,
	domain.AssetID,
) (domain.SourceGrantSummary, []byte, error) {
	return domain.SourceGrantSummary{}, nil, application.ErrSourceGrantNotFound
}

func (repository *MemoryProjects) ReadTranscript(
	context.Context,
	application.TranscriptReadQuery,
) (application.TranscriptReadPage, error) {
	return application.TranscriptReadPage{}, application.ErrTranscriptNotFound
}

func (repository *MemoryProjects) SelectTranscriptDefault(
	context.Context,
	application.SelectTranscriptDefaultRecord,
) (application.TranscriptDefaultSelection, error) {
	return application.TranscriptDefaultSelection{}, application.ErrTranscriptNotFound
}
