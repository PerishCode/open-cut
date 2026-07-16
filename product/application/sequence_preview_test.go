package application

import (
	"bytes"
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestSequencePreviewJobParametersCanonicalizeInputAndResourceSets(t *testing.T) {
	projectID := mustRenderProjectID(t, "00000000-0000-7000-8000-000000000001")
	sequenceID := mustRenderSequenceID(t, "00000000-0000-7000-8000-000000000002")
	firstClip := mustRenderClipID(t, "00000000-0000-7000-8000-000000000003")
	secondClip := mustRenderClipID(t, "00000000-0000-7000-8000-000000000004")
	firstStream := mustProxySourceStreamID(t, "00000000-0000-7000-8000-000000000005")
	secondStream := mustProxySourceStreamID(t, "00000000-0000-7000-8000-000000000006")
	firstJob, _ := domain.ParseWorkJobID("00000000-0000-7000-8000-000000000007")
	secondJob, _ := domain.ParseWorkJobID("00000000-0000-7000-8000-000000000008")
	parameters := SequencePreviewJobParameters{
		ProjectID: projectID, SequenceID: sequenceID, SequenceRevision: mustRenderRevision(t, 3),
		ResolverVersion: SequencePreviewResolverV1, CompilerVersion: domain.RenderPlanCompilerV4,
		RendererVersion: SequencePreviewRendererV1 + "@fixture", RendererTarget: "mac-arm64",
		OutputProfile: domain.SequencePreviewProfileV1,
		Inputs: []SequencePreviewInputRequirement{
			{ClipID: secondClip, SourceStreamID: secondStream, ProducerJobID: secondJob},
			{ClipID: firstClip, SourceStreamID: firstStream, ProducerJobID: firstJob},
		},
		Resources: []SequencePreviewResourcePin{
			{Kind: "font-bundle", ID: "open-cut-sans-cjk-v1", Version: "1", SHA256: renderDigest("f")},
		},
	}
	first, firstDigest, normalized, err := CanonicalSequencePreviewJobParameters(parameters)
	if err != nil {
		t.Fatal(err)
	}
	parameters.Inputs[0], parameters.Inputs[1] = parameters.Inputs[1], parameters.Inputs[0]
	second, secondDigest, _, err := CanonicalSequencePreviewJobParameters(parameters)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) || firstDigest != secondDigest || normalized.Inputs[0].ClipID != firstClip {
		t.Fatalf("non-canonical sequence preview parameters: %s / %s", first, second)
	}
}

func TestSequencePreviewRecoveryIsClosedOverTerminalFailureCodes(t *testing.T) {
	code := func(value string) *string { return &value }
	for _, fixture := range []struct {
		state domain.WorkJobState
		code  *string
		want  MediaRecoveryAction
	}{
		{state: domain.MediaJobCancelled, want: MediaRecoveryRetryJob},
		{state: domain.MediaJobFailed, code: code("renderer-failed"), want: MediaRecoveryRetryJob},
		{state: domain.MediaJobFailed, code: code("input-job-failed"), want: MediaRecoveryRelinkSource},
		{state: domain.MediaJobFailed, code: code("render-font-unavailable"), want: MediaRecoveryAcquireResource},
		{state: domain.MediaJobFailed, code: code("sequence-revision-conflict"), want: MediaRecoveryAdoptRevision},
		{state: domain.MediaJobFailed, code: code("renderer-output-invalid"), want: MediaRecoveryUpdateRuntime},
		{state: domain.MediaJobRunning, want: MediaRecoveryNone},
	} {
		job := SequencePreviewJobProjection{State: fixture.state, TerminalErrorCode: fixture.code}
		if got := SequencePreviewRecoveryAction(job); got != fixture.want {
			t.Fatalf("state=%s code=%v recovery=%s want=%s", fixture.state, fixture.code, got, fixture.want)
		}
	}
}
