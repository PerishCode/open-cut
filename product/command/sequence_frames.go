package command

import (
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type SequenceFramesInput struct {
	SequenceRevision *domain.Revision      `json:"sequenceRevision,omitempty" format:"uint64-decimal" doc:"Exact committed Sequence revision to prepare"`
	Times            []domain.RationalTime `json:"times,omitempty" minItems:"1" maxItems:"8" doc:"Strictly increasing exact Sequence times"`
	JobID            *domain.WorkJobID     `json:"jobId,omitempty" format:"uuid" doc:"Frame job lineage to continue"`
	RetryJobID       *domain.WorkJobID     `json:"retryJobId,omitempty" format:"uuid" doc:"Recoverable terminal frame job lineage to retry"`
}

func (input SequenceFramesInput) ApplicationInput() (application.SequenceFramesInput, error) {
	result := application.SequenceFramesInput{}
	switch {
	case input.SequenceRevision != nil && len(input.Times) > 0 && input.JobID == nil && input.RetryJobID == nil:
		result.Operation = application.SequenceFramesPrepare
		result.SequenceRevision = input.SequenceRevision
		result.Times = append([]domain.RationalTime(nil), input.Times...)
	case input.SequenceRevision == nil && len(input.Times) == 0 && input.JobID != nil && input.RetryJobID == nil:
		result.Operation = application.SequenceFramesContinue
		result.JobID = input.JobID
	case input.SequenceRevision == nil && len(input.Times) == 0 && input.JobID == nil && input.RetryJobID != nil:
		result.Operation = application.SequenceFramesRetry
		result.JobID = input.RetryJobID
	default:
		return application.SequenceFramesInput{}, application.ErrSequenceFramesInvalid
	}
	if err := result.Validate(); err != nil {
		return application.SequenceFramesInput{}, err
	}
	return result, nil
}

type SequenceFrameJobData struct {
	ID                  domain.WorkJobID    `json:"id" format:"uuid"`
	State               domain.WorkJobState `json:"state" enum:"blocked,queued,running,succeeded,failed,cancelled"`
	ProgressBasisPoints uint16              `json:"progressBasisPoints" minimum:"0" maximum:"10000"`
	TerminalErrorCode   *string             `json:"terminalErrorCode,omitempty" minLength:"1" maxLength:"256"`
	CreatedAt           time.Time           `json:"createdAt" format:"date-time"`
	UpdatedAt           time.Time           `json:"updatedAt" format:"date-time"`
}

type SequenceFramesData struct {
	Status           application.SequenceFrameSetStatus       `json:"status" enum:"accepted,ready,failed"`
	ProjectID        domain.ProjectID                         `json:"projectId" format:"uuid"`
	SequenceID       domain.SequenceID                        `json:"sequenceId" format:"uuid"`
	SequenceRevision domain.Revision                          `json:"sequenceRevision" format:"uint64-decimal"`
	Profile          string                                   `json:"profile" enum:"sequence-frame-srgb-png-v1"`
	Samples          []application.SequenceFrameCoordinate    `json:"samples" minItems:"1" maxItems:"8" nullable:"false"`
	Job              SequenceFrameJobData                     `json:"job"`
	Recovery         application.MediaRecoveryAction          `json:"recovery" enum:"retry-job,relink-source,acquire-resource,adopt-revision,update-runtime,none"`
	Resources        []application.SequenceFrameResourceLease `json:"resources" maxItems:"8" nullable:"false"`
	ActivityCursor   domain.Cursor                            `json:"activityCursor" format:"uint64-decimal"`
}

func SequenceFramesDataFrom(result application.SequenceFrameSetResult) SequenceFramesData {
	return SequenceFramesData{
		Status: result.Status, ProjectID: result.ProjectID, SequenceID: result.SequenceID,
		SequenceRevision: result.SequenceRevision, Profile: result.Profile,
		Samples: append([]application.SequenceFrameCoordinate(nil), result.Samples...),
		Job: SequenceFrameJobData{
			ID: result.Job.ID, State: result.Job.State,
			ProgressBasisPoints: result.Job.ProgressBasisPoints,
			TerminalErrorCode:   result.Job.TerminalErrorCode,
			CreatedAt:           result.Job.CreatedAt.UTC(), UpdatedAt: result.Job.UpdatedAt.UTC(),
		},
		Recovery:       result.Recovery,
		Resources:      append([]application.SequenceFrameResourceLease(nil), result.Resources...),
		ActivityCursor: result.ActivityCursor,
	}
}

func (result SequenceFramesData) Validate() error {
	if result.ProjectID.IsZero() || result.SequenceID.IsZero() || result.SequenceRevision.Value() == 0 ||
		result.Profile != application.SequenceFrameSetProfile || result.Job.ID.IsZero() ||
		result.Job.CreatedAt.IsZero() || result.Job.UpdatedAt.IsZero() || result.Job.ProgressBasisPoints > 10_000 ||
		len(result.Samples) == 0 || len(result.Samples) > application.MaximumSequenceFrameSamples ||
		len(result.Resources) > application.MaximumSequenceFrameSamples || result.ActivityCursor == 0 ||
		!validSequenceFrameRecovery(result.Recovery) {
		return fmt.Errorf("invalid sequence frame result")
	}
	for index, sample := range result.Samples {
		if sample.RequestedTime.Validate() != nil || sample.RequestedTime.IsNegative() ||
			sample.SequenceTime.Validate() != nil || sample.SequenceTime.IsNegative() {
			return fmt.Errorf("invalid sequence frame coordinate")
		}
		if index > 0 {
			comparison, err := result.Samples[index-1].RequestedTime.Compare(sample.RequestedTime)
			if err != nil || comparison >= 0 {
				return fmt.Errorf("invalid sequence frame order")
			}
		}
	}
	switch result.Status {
	case application.SequenceFrameSetAccepted:
		if result.Job.State != domain.MediaJobBlocked && result.Job.State != domain.MediaJobQueued &&
			result.Job.State != domain.MediaJobRunning || result.Job.TerminalErrorCode != nil ||
			result.Recovery != application.MediaRecoveryNone || len(result.Resources) != 0 {
			return fmt.Errorf("invalid accepted sequence frame result")
		}
	case application.SequenceFrameSetReady:
		if result.Job.State != domain.MediaJobSucceeded || result.Job.TerminalErrorCode != nil ||
			result.Recovery != application.MediaRecoveryNone || len(result.Resources) != len(result.Samples) {
			return fmt.Errorf("invalid ready sequence frame result")
		}
		for index, resource := range result.Resources {
			if resource.ResourceID.IsZero() || resource.MIMEType != "image/png" || resource.ByteSize.Value() == 0 ||
				resource.SHA256 == "" || resource.ReadOnlyPath == "" || resource.ExpiresAt.IsZero() ||
				resource.RequestedTime != result.Samples[index].RequestedTime ||
				resource.SequenceTime != result.Samples[index].SequenceTime ||
				resource.FrameIndex != result.Samples[index].FrameIndex {
				return fmt.Errorf("invalid sequence frame resource")
			}
		}
	case application.SequenceFrameSetFailed:
		if result.Job.TerminalErrorCode == nil || *result.Job.TerminalErrorCode == "" || len(result.Resources) != 0 {
			return fmt.Errorf("invalid failed sequence frame result")
		}
		if result.Job.State != domain.MediaJobFailed && result.Job.State != domain.MediaJobCancelled &&
			result.Job.State != domain.MediaJobSucceeded {
			return fmt.Errorf("invalid terminal sequence frame job")
		}
	default:
		return fmt.Errorf("invalid sequence frame status")
	}
	return nil
}

func validSequenceFrameRecovery(value application.MediaRecoveryAction) bool {
	switch value {
	case application.MediaRecoveryRetryJob, application.MediaRecoveryRelinkSource,
		application.MediaRecoveryAcquireResource, application.MediaRecoveryAdoptRevision,
		application.MediaRecoveryUpdateRuntime, application.MediaRecoveryNone:
		return true
	default:
		return false
	}
}
