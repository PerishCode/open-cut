package application

import (
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestSequenceFrameCoordinatesFloorToExactFrameGrid(t *testing.T) {
	frameRate, _ := domain.NewRationalTime(30_000, 1001)
	fixtures := []struct {
		requested     domain.RationalTime
		wantIndex     uint64
		wantGridValue int64
		wantGridScale int32
	}{
		{requested: mustSequenceFrameTime(t, 0, 1), wantIndex: 0, wantGridValue: 0, wantGridScale: 1},
		{requested: mustSequenceFrameTime(t, 1001, 60_000), wantIndex: 0, wantGridValue: 0, wantGridScale: 1},
		{requested: mustSequenceFrameTime(t, 1001, 30_000), wantIndex: 1, wantGridValue: 1001, wantGridScale: 30_000},
		{requested: mustSequenceFrameTime(t, 1, 10), wantIndex: 2, wantGridValue: 1001, wantGridScale: 15_000},
	}
	for _, fixture := range fixtures {
		coordinate, err := sequenceFrameCoordinate(fixture.requested, frameRate)
		if err != nil {
			t.Fatal(err)
		}
		if coordinate.RequestedTime != fixture.requested || coordinate.FrameIndex.Value() != fixture.wantIndex ||
			coordinate.SequenceTime.Value.Value() != fixture.wantGridValue ||
			coordinate.SequenceTime.Scale != fixture.wantGridScale {
			t.Fatalf("requested=%+v coordinate=%+v", fixture.requested, coordinate)
		}
	}
}

func TestSequenceFramesInputIsAClosedOperationUnion(t *testing.T) {
	revision, _ := domain.NewRevision(3)
	instant := mustSequenceFrameTime(t, 1, 2)
	jobID, _ := domain.ParseWorkJobID("018f0000-0000-7000-8000-000000000001")
	valid := []SequenceFramesInput{
		{Operation: SequenceFramesPrepare, SequenceRevision: &revision, Times: []domain.RationalTime{instant}},
		{Operation: SequenceFramesContinue, JobID: &jobID},
		{Operation: SequenceFramesRetry, JobID: &jobID},
	}
	for _, input := range valid {
		if err := input.Validate(); err != nil {
			t.Fatalf("valid input=%+v err=%v", input, err)
		}
	}
	invalid := []SequenceFramesInput{
		{Operation: SequenceFramesPrepare, SequenceRevision: &revision},
		{Operation: SequenceFramesPrepare, SequenceRevision: &revision, Times: []domain.RationalTime{instant, instant}},
		{Operation: SequenceFramesContinue, SequenceRevision: &revision, JobID: &jobID},
		{Operation: SequenceFramesRetry},
	}
	for _, input := range invalid {
		if err := input.Validate(); err == nil {
			t.Fatalf("invalid input accepted: %+v", input)
		}
	}
}

func TestSequenceFrameRecoveryIsClosedOverTerminalCodes(t *testing.T) {
	code := func(value string) *string { return &value }
	fixtures := []struct {
		job  SequenceFrameJob
		want MediaRecoveryAction
	}{
		{job: SequenceFrameJob{State: domain.MediaJobCancelled}, want: MediaRecoveryRetryJob},
		{job: SequenceFrameJob{State: domain.MediaJobFailed, TerminalErrorCode: code("frame-decode-failed")}, want: MediaRecoveryRetryJob},
		{job: SequenceFrameJob{State: domain.MediaJobFailed, TerminalErrorCode: code("input-job-failed")}, want: MediaRecoveryRelinkSource},
		{job: SequenceFrameJob{State: domain.MediaJobFailed, TerminalErrorCode: code("sequence-time-out-of-range")}, want: MediaRecoveryNone},
		{job: SequenceFrameJob{State: domain.MediaJobFailed, TerminalErrorCode: code("unknown-runtime-failure")}, want: MediaRecoveryUpdateRuntime},
		{job: SequenceFrameJob{State: domain.MediaJobSucceeded}, want: MediaRecoveryNone},
	}
	for _, fixture := range fixtures {
		if got := SequenceFrameRecoveryAction(fixture.job); got != fixture.want {
			t.Fatalf("job=%+v got=%s want=%s", fixture.job, got, fixture.want)
		}
	}
}

func mustSequenceFrameTime(t *testing.T, value int64, scale int32) domain.RationalTime {
	t.Helper()
	result, err := domain.NewRationalTime(value, scale)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
