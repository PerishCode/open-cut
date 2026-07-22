package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/repository"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestSQLiteCreatorClipPlacementCommitsLinkedAVAtAbsoluteSourceTime(t *testing.T) {
	parallelAPITest(t)
	ctx := context.Background()
	store, err := repository.OpenSQLiteProjects(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projects, _, _, _ := testProjectApplications(t, store)
	creator := creatorContext(t)
	created, err := projects.Create(creator, application.CreateProjectInput{
		RequestID: mustRequestID(t, "ui:placement-project"), Name: "Direct source placement",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	clock := application.ClockFunc(func() time.Time { return now })
	media, err := application.NewMedia(store, application.UUIDv7IdentityGenerator{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	size, _ := domain.NewUInt64(4096)
	observation := domain.SourceObservation{
		ByteSize: size, ModifiedUnixNs: domain.NewInt64(1234), FileIdentity: "fixture:creator-placement",
	}
	grant, err := media.RegisterSourceGrant(creator, application.RegisterSourceGrantInput{
		RequestID: mustRequestID(t, "picker:placement-source"), Platform: "mac",
		Kind: domain.SourceGrantLocalPath, DisplayName: "placement.mov", Observation: observation,
		ProtectedMaterial: []byte(`{"schema":"open-cut/source-grant-material/local-path/v1","path":"/fixture/placement.mov"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := media.RegisterAsset(creator, created.Project.Project.ID, application.RegisterAssetInput{
		RequestID: mustRequestID(t, "ui:placement-asset"), SourceGrantID: grant.Grant.ID,
		ImportMode: domain.AssetReferenced, ExpectedProjectRevision: created.Project.Project.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := testRenderDigest("7")
	sourceStart := mustRational(t, -2, 1)
	sourceDuration := mustRational(t, 5, 1)
	videoTimeBase := mustRational(t, 1, 1000)
	audioTimeBase := mustRational(t, 1, 48_000)
	scheduler := newTestWorkScheduler(t, store, []application.MediaJobExecutor{
		fixedIdentifyExecutor{result: application.MediaIdentification{Fingerprint: fingerprint, Observation: observation}},
		fixedProbeExecutor{result: application.MediaProbe{
			Container: "matroska", StartTime: &sourceStart, Duration: &sourceDuration,
			Streams: []domain.SourceStreamDescriptor{
				{Index: 0, MediaType: domain.MediaVideo, Codec: "vp9", TimeBase: videoTimeBase,
					StartTime: &sourceStart, Duration: &sourceDuration, Dispositions: []string{"default"},
					Video: &domain.VideoStreamFacts{Width: 1920, Height: 1080, Rotation: 0}},
				{Index: 1, MediaType: domain.MediaAudio, Codec: "opus", TimeBase: audioTimeBase,
					StartTime: &sourceStart, Duration: &sourceDuration, Dispositions: []string{"default"},
					Audio: &domain.AudioStreamFacts{SampleRate: 48_000, Channels: 2, ChannelLayout: "stereo"}},
			},
		}},
	}, clock, "api:creator-placement-test")
	for step := 0; step < 2; step++ {
		if executed, runErr := scheduler.RunOne(ctx); runErr != nil || !executed {
			t.Fatalf("media step %d executed=%v err=%v", step, executed, runErr)
		}
	}
	assetReads, _ := application.NewAssetReads(store)
	asset, _, err := assetReads.Inspect(creator, created.Project.Project.ID, registered.Asset.Asset.ID)
	if err != nil || asset.Facts == nil || asset.AcceptedFingerprint == nil {
		t.Fatalf("asset=%+v err=%v", asset, err)
	}
	var videoStream, audioStream domain.SourceStreamID
	for _, stream := range asset.Facts.Streams {
		switch stream.Descriptor.MediaType {
		case domain.MediaVideo:
			videoStream = stream.ID
		case domain.MediaAudio:
			audioStream = stream.ID
		}
	}
	var videoTrack, audioTrack application.TrackSummary
	for _, track := range created.Project.Tracks {
		switch track.Type {
		case domain.TrackVideo:
			videoTrack = track
		case domain.TrackAudio:
			audioTrack = track
		}
	}
	if videoStream.IsZero() || audioStream.IsZero() || videoTrack.ID.IsZero() || audioTrack.ID.IsZero() {
		t.Fatalf("tracks=%+v streams=%s/%s", created.Project.Tracks, videoStream, audioStream)
	}
	reads, err := application.NewEditReads(store)
	if err != nil {
		t.Fatal(err)
	}
	edits, err := application.NewEdits(store, application.UUIDv7IdentityGenerator{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	prefix, _ := domain.ParseLocalID("source_place")
	clipDuration := mustRational(t, 1, 1)
	sourceRange := domain.TimeRange{Start: sourceStart, Duration: clipDuration}
	input := application.CreatorClipPlacementPreviewInput{
		AssetID: asset.ID, AssetRevision: asset.Revision, AcceptedFingerprint: *asset.AcceptedFingerprint,
		SourceRange: sourceRange, TimelineStart: mustRational(t, 0, 1), LocalPrefix: prefix,
		Video: &application.CreatorClipPlacementLaneInput{
			TrackID: videoTrack.ID, TrackRevision: videoTrack.Revision, SourceStreamID: videoStream,
		},
		Audio: &application.CreatorClipPlacementLaneInput{
			TrackID: audioTrack.ID, TrackRevision: audioTrack.Revision, SourceStreamID: audioStream,
		},
	}
	proposalsBefore, transactionsBefore := editJournalCounts(t, store.Path())
	preview, err := reads.ClipPlacementForCreator(
		creator, created.Project.Project.ID, created.Project.Project.MainSequenceID, input,
	)
	if err != nil || !preview.Linked || len(preview.Lanes) != 2 || len(preview.Operations) != 2 ||
		preview.Operations[0].CreateLinkGroupAs == nil || preview.Operations[1].LinkGroup == nil ||
		preview.OutputDigest == "" || preview.SourceRange.Start != sourceStart ||
		preview.TimelineRange.Start != input.TimelineStart {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	proposalsAfter, transactionsAfter := editJournalCounts(t, store.Path())
	if proposalsBefore != proposalsAfter || transactionsBefore != transactionsAfter {
		t.Fatalf("preview wrote journal state: proposals %d→%d transactions %d→%d",
			proposalsBefore, proposalsAfter, transactionsBefore, transactionsAfter)
	}
	stale := input
	stale.AcceptedFingerprint = testRenderDigest("8")
	if _, err := reads.ClipPlacementForCreator(
		creator, created.Project.Project.ID, created.Project.Project.MainSequenceID, stale,
	); !errors.Is(err, application.ErrEditConflict) {
		t.Fatalf("stale fingerprint error=%v", err)
	}
	committed, err := edits.CommitForCreator(
		creator, created.Project.Project.ID, created.Project.Project.MainSequenceID,
		application.EditProposeInput{
			RequestID: mustRequestID(t, "gesture:creator-source-place"), Intent: "Place selected source range",
			BaseProjectRevision: preview.BaseProjectRevision,
			Preconditions:       preview.Preconditions, Operations: preview.Operations,
		},
	)
	if err != nil || len(committed.Transaction.Operations) != 3 ||
		committed.Transaction.CommittedProjectRevision.Value() != preview.BaseProjectRevision.Value()+1 {
		t.Fatalf("commit=%+v err=%v", committed, err)
	}
	videoID, err := domain.ParseClipID(allocationID(t, committed.Proposal, *preview.Operations[0].CreateAs))
	if err != nil {
		t.Fatal(err)
	}
	audioID, err := domain.ParseClipID(allocationID(t, committed.Proposal, *preview.Operations[1].CreateAs))
	if err != nil {
		t.Fatal(err)
	}
	video := readPlacedClipEntity(t, reads, creator, created.Project.Project.ID, videoID)
	audio := readPlacedClipEntity(t, reads, creator, created.Project.Project.ID, audioID)
	if !sameTestRange(video.SourceRange, sourceRange) || !sameTestRange(audio.SourceRange, sourceRange) ||
		video.LinkGroupID == nil || audio.LinkGroupID == nil || *video.LinkGroupID != *audio.LinkGroupID {
		t.Fatalf("video=%+v audio=%+v", video, audio)
	}
	input.Video.TrackRevision, _ = domain.NewRevision(videoTrack.Revision.Value() + 1)
	input.Audio.TrackRevision, _ = domain.NewRevision(audioTrack.Revision.Value() + 1)
	if _, err := reads.ClipPlacementForCreator(
		creator, created.Project.Project.ID, created.Project.Project.MainSequenceID, input,
	); !errors.Is(err, application.ErrEditInvalid) {
		t.Fatalf("collision error=%v", err)
	}
	undone, err := edits.UndoForCreator(
		creator, created.Project.Project.ID, created.Project.Project.MainSequenceID, committed.Transaction.ID,
		application.EditUndoInput{RequestID: mustRequestID(t, "gesture:creator-source-place-undo")},
	)
	if err != nil || !readPlacedClipEntity(t, reads, creator, created.Project.Project.ID, videoID).Tombstoned ||
		!readPlacedClipEntity(t, reads, creator, created.Project.Project.ID, audioID).Tombstoned {
		t.Fatalf("undo=%+v err=%v", undone, err)
	}
}

func readPlacedClipEntity(
	t *testing.T,
	reads *application.EditReads,
	ctx context.Context,
	projectID domain.ProjectID,
	id domain.ClipID,
) domain.ClipState {
	t.Helper()
	result, err := reads.Entity(ctx, projectID, domain.EntityClip, id.String())
	if err != nil || result.Clip == nil {
		t.Fatalf("clip=%+v err=%v", result, err)
	}
	return *result.Clip
}
