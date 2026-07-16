package command

import (
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type ExportStartInput struct {
	RequestID        domain.RequestID `json:"requestId" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	SequenceRevision domain.Revision  `json:"sequenceRevision" format:"uint64-decimal"`
	Preset           string           `json:"preset" enum:"webm-vp9-opus-v1"`
}

func (input ExportStartInput) ApplicationInput() (application.SequenceExportStartInput, error) {
	result := application.SequenceExportStartInput{
		RequestID: input.RequestID, SequenceRevision: input.SequenceRevision, Preset: input.Preset,
	}
	return result, result.Validate()
}

type ExportShowInput struct {
	JobID domain.WorkJobID `json:"jobId" format:"uuid"`
}

type ExportRetryInput = ExportShowInput

type ExportCancelInput struct {
	RequestID domain.RequestID `json:"requestId" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"`
	JobID     domain.WorkJobID `json:"jobId" format:"uuid"`
}

type ExportJobData struct {
	ID                  domain.WorkJobID    `json:"id" format:"uuid"`
	RootJobID           domain.WorkJobID    `json:"rootJobId" format:"uuid"`
	RetryOfJobID        *domain.WorkJobID   `json:"retryOfJobId,omitempty" format:"uuid"`
	State               domain.WorkJobState `json:"state" enum:"blocked,queued,running,succeeded,failed,cancelled"`
	ProgressBasisPoints uint16              `json:"progressBasisPoints" minimum:"0" maximum:"10000"`
	TerminalErrorCode   *string             `json:"terminalErrorCode,omitempty" minLength:"1" maxLength:"64"`
	CreatedAt           time.Time           `json:"createdAt" format:"date-time"`
	UpdatedAt           time.Time           `json:"updatedAt" format:"date-time"`
}

type ExportArtifactData struct {
	ID                   domain.ArtifactID   `json:"id" format:"uuid"`
	Verification         string              `json:"verification" enum:"passed"`
	SemanticDuration     domain.RationalTime `json:"semanticDuration"`
	PresentationDuration domain.RationalTime `json:"presentationDuration"`
	CanvasWidth          uint32              `json:"canvasWidth" minimum:"2"`
	CanvasHeight         uint32              `json:"canvasHeight" minimum:"2"`
	FrameRate            domain.RationalTime `json:"frameRate"`
	VideoFrameCount      domain.UInt64       `json:"videoFrameCount" format:"uint64-decimal"`
	AudioSampleRate      uint32              `json:"audioSampleRate" enum:"48000"`
	AudioSampleCount     domain.UInt64       `json:"audioSampleCount" format:"uint64-decimal"`
	VideoCodec           string              `json:"videoCodec" enum:"vp9"`
	AudioCodec           string              `json:"audioCodec" enum:"opus"`
	PixelFormat          string              `json:"pixelFormat" enum:"yuv420p"`
	ChannelLayout        string              `json:"channelLayout" enum:"stereo"`
	ByteSize             domain.UInt64       `json:"byteSize" format:"uint64-decimal"`
	ContentDigest        domain.Digest       `json:"contentDigest"`
}

type ExportData struct {
	ProjectID        domain.ProjectID                `json:"projectId" format:"uuid"`
	SequenceID       domain.SequenceID               `json:"sequenceId" format:"uuid"`
	SequenceRevision domain.Revision                 `json:"sequenceRevision" format:"uint64-decimal"`
	Preset           string                          `json:"preset" enum:"webm-vp9-opus-v1"`
	Job              ExportJobData                   `json:"job"`
	Artifact         *ExportArtifactData             `json:"artifact,omitempty"`
	Recovery         application.MediaRecoveryAction `json:"recovery" enum:"retry-job,relink-source,acquire-resource,adopt-revision,update-runtime,none"`
	Replayed         bool                            `json:"replayed"`
	ActivityCursor   domain.Cursor                   `json:"activityCursor" format:"uint64-decimal"`
}

type ExportLineageData struct {
	Origin               application.SequenceExportOrigin               `json:"origin" enum:"agent,creator"`
	AttemptCount         domain.UInt64                                  `json:"attemptCount" format:"uint64-decimal"`
	ArtifactAvailability application.SequenceExportArtifactAvailability `json:"artifactAvailability" enum:"none,ready,invalid,deleted"`
	RootCreatedAt        time.Time                                      `json:"rootCreatedAt" format:"date-time"`
	Export               ExportData                                     `json:"export"`
}

type ExportHistoryData struct {
	Lineages       []ExportLineageData `json:"lineages" maxItems:"50" nullable:"false"`
	NextAfter      string              `json:"nextAfter,omitempty" maxLength:"512"`
	ActivityCursor domain.Cursor       `json:"activityCursor" format:"uint64-decimal"`
}

func ExportHistoryDataFrom(result application.ListSequenceExportHistoryResult) ExportHistoryData {
	lineages := make([]ExportLineageData, 0, len(result.Lineages))
	for _, lineage := range result.Lineages {
		lineages = append(lineages, ExportLineageData{
			Origin: lineage.Origin, AttemptCount: lineage.AttemptCount,
			ArtifactAvailability: lineage.ArtifactAvailability,
			RootCreatedAt:        lineage.RootCreatedAt.UTC(), Export: ExportDataFrom(lineage.Export),
		})
	}
	return ExportHistoryData{
		Lineages: lineages, NextAfter: result.NextAfter, ActivityCursor: result.ActivityCursor,
	}
}

func ExportDataFrom(result application.SequenceExportResult) ExportData {
	data := ExportData{
		ProjectID: result.ProjectID, SequenceID: result.SequenceID,
		SequenceRevision: result.SequenceRevision, Preset: result.Preset,
		Job: ExportJobData{
			ID: result.Job.ID, RootJobID: result.Job.RootJobID, RetryOfJobID: result.Job.RetryOfJobID,
			State: result.Job.State, ProgressBasisPoints: result.Job.ProgressBasisPoints,
			TerminalErrorCode: result.Job.TerminalErrorCode,
			CreatedAt:         result.Job.CreatedAt.UTC(), UpdatedAt: result.Job.UpdatedAt.UTC(),
		},
		Recovery: result.Recovery, Replayed: result.Replayed, ActivityCursor: result.ActivityCursor,
	}
	if result.Job.Artifact != nil && result.Job.Artifact.State == domain.SequenceExportArtifactValid {
		artifact, facts := result.Job.Artifact, result.Job.Artifact.Facts
		data.Artifact = &ExportArtifactData{
			ID: artifact.ID, Verification: "passed",
			SemanticDuration: facts.SemanticDuration, PresentationDuration: facts.PresentationDuration,
			CanvasWidth: facts.CanvasWidth, CanvasHeight: facts.CanvasHeight, FrameRate: facts.FrameRate,
			VideoFrameCount: facts.VideoFrameCount, AudioSampleRate: facts.AudioSampleRate,
			AudioSampleCount: facts.AudioSampleCount, VideoCodec: facts.VideoCodec,
			AudioCodec: facts.AudioCodec, PixelFormat: facts.PixelFormat,
			ChannelLayout: facts.ChannelLayout, ByteSize: artifact.ByteSize,
			ContentDigest: artifact.ContentDigest,
		}
	}
	return data
}

func (data ExportData) Validate() error {
	job := data.Job
	if data.ProjectID.IsZero() || data.SequenceID.IsZero() || data.SequenceRevision.Value() == 0 ||
		data.Preset != domain.SequenceExportProfileV1 || job.ID.IsZero() || job.RootJobID.IsZero() ||
		job.ProgressBasisPoints > 10_000 || job.CreatedAt.IsZero() || job.UpdatedAt.IsZero() ||
		data.ActivityCursor == 0 || !validSequenceFrameRecovery(data.Recovery) {
		return fmt.Errorf("invalid export result")
	}
	if job.RetryOfJobID != nil && job.RetryOfJobID.IsZero() ||
		job.TerminalErrorCode != nil && *job.TerminalErrorCode == "" {
		return fmt.Errorf("invalid export job")
	}
	switch job.State {
	case domain.MediaJobBlocked, domain.MediaJobQueued, domain.MediaJobRunning:
		if job.TerminalErrorCode != nil || data.Artifact != nil || data.Recovery != application.MediaRecoveryNone {
			return fmt.Errorf("invalid active export result")
		}
	case domain.MediaJobSucceeded:
		if job.TerminalErrorCode != nil {
			return fmt.Errorf("invalid successful export result")
		}
		if data.Artifact == nil && data.Recovery != application.MediaRecoveryRetryJob {
			return fmt.Errorf("invalid export artifact recovery")
		}
	case domain.MediaJobFailed:
		if job.TerminalErrorCode == nil || data.Artifact != nil {
			return fmt.Errorf("invalid failed export result")
		}
	case domain.MediaJobCancelled:
		if job.TerminalErrorCode != nil || data.Artifact != nil || data.Recovery != application.MediaRecoveryRetryJob {
			return fmt.Errorf("invalid cancelled export result")
		}
	default:
		return fmt.Errorf("invalid export job state")
	}
	if data.Artifact != nil {
		artifact := data.Artifact
		facts := domain.RenderedMediaFacts{
			SemanticDuration: artifact.SemanticDuration, PresentationDuration: artifact.PresentationDuration,
			CanvasWidth: artifact.CanvasWidth, CanvasHeight: artifact.CanvasHeight,
			FrameRate: artifact.FrameRate, VideoFrameCount: artifact.VideoFrameCount,
			AudioSampleRate: artifact.AudioSampleRate, AudioSampleCount: artifact.AudioSampleCount,
			VideoCodec: artifact.VideoCodec, AudioCodec: artifact.AudioCodec,
			PixelFormat: artifact.PixelFormat, ChannelLayout: artifact.ChannelLayout,
		}
		if artifact.ID.IsZero() || artifact.Verification != "passed" || artifact.ByteSize.Value() == 0 ||
			application.ValidateSequenceExportFacts(facts) != nil {
			return fmt.Errorf("invalid export artifact")
		}
		if _, err := domain.ParseDigest(artifact.ContentDigest.String()); err != nil {
			return fmt.Errorf("invalid export content digest")
		}
	}
	return nil
}
