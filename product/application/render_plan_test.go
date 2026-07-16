package application

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestSequencePreviewPlanPinsExactLinkedAVInputsDeterministically(t *testing.T) {
	fixture := renderPlanFixture(t)
	first, err := CompileSequencePreviewPlan(fixture)
	if err != nil {
		t.Fatal(err)
	}
	fixture.Clips[0], fixture.Clips[1] = fixture.Clips[1], fixture.Clips[0]
	fixture.Bindings[0], fixture.Bindings[1] = fixture.Bindings[1], fixture.Bindings[0]
	fixture.ObservedProjectRevision = mustRenderRevision(t, 9)
	second, err := CompileSequencePreviewPlan(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !EqualCompiledRenderPlans(first, second) {
		t.Fatal("input ordering or observed Project revision changed the semantic plan")
	}
	plan := first.Plan.Payload
	pinnedMaterial := fixture.Bindings[0].Material
	if len(plan.Inputs) != 1 || len(plan.Video) != 1 || len(plan.Audio) != 1 || len(plan.Captions) != 0 ||
		plan.Duration.Value.Value() != 5 || plan.Duration.Scale != 1 ||
		plan.Output.CanvasWidth != 1280 || plan.Output.CanvasHeight != 720 ||
		plan.Output.VideoFrameCount.Value() != 150 || plan.Output.AudioSampleCount.Value() != 240_000 ||
		plan.Output.StreamPolicy != "single-video-single-audio-v1" ||
		plan.Output.VideoSamplingPolicy != "source-map-floor-first-fallback-v1" ||
		plan.Output.AudioSamplingPolicy != "render-material-sample-floor-silence-v1" ||
		plan.Output.TailPolicy != "ceil-pad-black-silence-v1" ||
		plan.Output.Evaluation.CoordinatePolicy != domain.RenderCoordinatePolicyV1 ||
		plan.Output.Evaluation.ColorPipeline != domain.RenderColorPipelineV1 ||
		plan.Output.Evaluation.ScalePolicy != domain.RenderScalePolicyV1 ||
		plan.Output.Evaluation.BlendPolicy != domain.RenderBlendPolicyV1 ||
		plan.Output.Evaluation.AudioGainPolicy != domain.RenderAudioGainPolicyV1 ||
		plan.Output.Evaluation.AudioMixPolicy != domain.RenderAudioMixPolicyV1 ||
		plan.Output.Evaluation.CaptionRasterPolicy != domain.RenderCaptionRasterPolicyV1 ||
		plan.Output.Evaluation.DeterminismPolicy != domain.RenderDeterminismPolicyV1 ||
		plan.Output.Mux.MuxPolicy != domain.RenderMuxPolicyV1 ||
		plan.Output.Mux.KeyframePolicy != domain.RenderKeyframePolicyV1 ||
		plan.Output.Mux.OpusTrimPolicy != domain.RenderOpusTrimPolicyV1 ||
		plan.Output.Video.ChromaLocation != "left" || plan.Output.Audio.PCMFormat != "s16" ||
		plan.Output.Audio.DitherPolicy != "none-v1" ||
		plan.Inputs[0].Video == nil || plan.Inputs[0].Audio == nil ||
		plan.Inputs[0].Video.SourceTimeBase != pinnedMaterial.video.SourceTimeBase ||
		plan.Inputs[0].Video.MaterialTimeBase != pinnedMaterial.video.MaterialTimeBase ||
		plan.Inputs[0].Audio.SourceTimeBase != pinnedMaterial.audio.SourceTimeBase ||
		plan.Inputs[0].Audio.MaterialTimeBase != pinnedMaterial.audio.MaterialTimeBase ||
		plan.Video[0].SourceStreamID != plan.Inputs[0].Video.SourceStreamID ||
		plan.Audio[0].SourceStreamID != plan.Inputs[0].Audio.SourceStreamID {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if strings.Contains(string(first.Canonical), "linkGroup") ||
		strings.Contains(string(first.Canonical), "observedProjectRevision") {
		t.Fatalf("edit-only or observed Project metadata leaked into semantic plan: %s", first.Canonical)
	}
}

func TestSequencePreviewPlanPreservesAbsoluteNonzeroSourceCoordinate(t *testing.T) {
	fixture := renderPlanFixture(t)
	sourceStart := testRational(t, 10, 1)
	duration := testRational(t, 5, 1)
	for index := range fixture.Clips {
		fixture.Clips[index].SourceRange = domain.TimeRange{Start: sourceStart, Duration: duration}
	}
	for index := range fixture.Bindings {
		fixture.Bindings[index].Material.sourceEpoch = sourceStart
		if fixture.Bindings[index].Material.video != nil {
			fixture.Bindings[index].Material.video.SourceStart = sourceStart
		}
		if fixture.Bindings[index].Material.audio != nil {
			fixture.Bindings[index].Material.audio.SourceStart = sourceStart
		}
	}

	compiled, err := CompileSequencePreviewPlan(fixture)
	if err != nil {
		t.Fatal(err)
	}
	plan := compiled.Plan.Payload
	if len(plan.Inputs) != 1 || plan.Inputs[0].SourceEpoch != sourceStart ||
		plan.Inputs[0].Video == nil || plan.Inputs[0].Video.SourceStart != sourceStart ||
		plan.Inputs[0].Audio == nil || plan.Inputs[0].Audio.SourceStart != sourceStart ||
		len(plan.Video) != 1 || plan.Video[0].SourceRange.Start != sourceStart ||
		len(plan.Audio) != 1 || plan.Audio[0].SourceRange.Start != sourceStart {
		t.Fatalf("nonzero source coordinate was rebased: %+v", plan)
	}
}

func TestSequenceExportPlanSharesInstructionsButPinsFullQualityMaterials(t *testing.T) {
	preview, err := CompileSequencePreviewPlan(renderPlanFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	exportFixture := renderExportPlanFixture(t)
	exported, err := CompileSequenceExportPlan(exportFixture)
	if err != nil {
		t.Fatal(err)
	}
	plan := exported.Plan.Payload
	if plan.Purpose != domain.RenderPurposeExport || plan.Output.Profile != domain.SequenceExportProfileV1 ||
		plan.Output.CanvasWidth != exportFixture.Sequence.Format.CanvasWidth ||
		plan.Output.CanvasHeight != exportFixture.Sequence.Format.CanvasHeight ||
		plan.Output.Video.CRF != 24 || plan.Output.Video.CPUUsed != 2 ||
		plan.Output.Audio.BitRate != 192_000 || len(plan.Inputs) != 2 ||
		plan.Inputs[0].Profile != RenderInputProfile || plan.Inputs[1].Profile != RenderInputProfile ||
		len(plan.Video) != len(preview.Plan.Payload.Video) || len(plan.Audio) != len(preview.Plan.Payload.Audio) ||
		plan.Video[0].ClipID != preview.Plan.Payload.Video[0].ClipID ||
		plan.Video[0].SourceRange != preview.Plan.Payload.Video[0].SourceRange ||
		plan.Video[0].TimelineRange != preview.Plan.Payload.Video[0].TimelineRange ||
		plan.Audio[0].ClipID != preview.Plan.Payload.Audio[0].ClipID ||
		plan.Audio[0].SourceRange != preview.Plan.Payload.Audio[0].SourceRange ||
		plan.Audio[0].TimelineRange != preview.Plan.Payload.Audio[0].TimelineRange {
		t.Fatalf("unexpected export plan: %+v", plan)
	}
	if exported.Plan.Digest == preview.Plan.Digest {
		t.Fatal("preview and export produced the same semantic plan digest")
	}
	if _, err := CompileSequencePreviewPlan(exportFixture); !errors.Is(err, ErrRenderInputRequired) {
		t.Fatalf("preview accepted render-input material: %v", err)
	}
	if _, err := CompileSequenceExportPlan(renderPlanFixture(t)); !errors.Is(err, ErrRenderInputRequired) {
		t.Fatalf("export accepted Viewer proxy material: %v", err)
	}
	exportFixture.Sequence.Format.CanvasWidth++
	if _, err := CompileSequenceExportPlan(exportFixture); !errors.Is(err, ErrRenderPlanInvalid) {
		t.Fatalf("export accepted odd canvas: %v", err)
	}
}

func TestPublishedSequencePreviewPlanRejectsHiddenRendererDefaults(t *testing.T) {
	compiled, err := CompileSequencePreviewPlan(renderPlanFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*domain.RenderPlanPayload){
		func(plan *domain.RenderPlanPayload) { plan.Output.Evaluation.ScalePolicy = "ambient" },
		func(plan *domain.RenderPlanPayload) { plan.Output.Mux.KeyframePolicy = "scene-cut" },
		func(plan *domain.RenderPlanPayload) { plan.Output.Audio.DitherPolicy = "host-default" },
		func(plan *domain.RenderPlanPayload) {
			plan.Video[0].Placement.TranslateX = domain.ExactRational{Value: 1, Scale: 0}
		},
		func(plan *domain.RenderPlanPayload) { plan.Audio[0].GainMilliDB = domain.RenderGainMaximumMilliDB + 1 },
	}
	for index, mutate := range mutations {
		plan := compiled.Plan.Payload
		plan.Inputs = append([]domain.RenderPlanInput(nil), plan.Inputs...)
		plan.Video = append([]domain.RenderVideoInstruction(nil), plan.Video...)
		plan.Audio = append([]domain.RenderAudioInstruction(nil), plan.Audio...)
		mutate(&plan)
		if err := ValidateSequencePreviewRenderPlanPayload(plan); !errors.Is(err, ErrRenderPlanInvalid) {
			t.Fatalf("mutation %d was accepted: %v", index, err)
		}
	}
}

func TestSequencePreviewPlanCeilsPhysicalGridsWithoutChangingSemanticDuration(t *testing.T) {
	fixture := renderPlanFixture(t)
	duration, _ := domain.NewRationalTime(1, 100)
	for index := range fixture.Clips {
		fixture.Clips[index].SourceRange.Duration = duration
		fixture.Clips[index].TimelineRange.Duration = duration
	}
	compiled, err := CompileSequencePreviewPlan(fixture)
	if err != nil {
		t.Fatal(err)
	}
	plan := compiled.Plan.Payload
	if plan.Duration != duration || plan.Output.VideoFrameCount.Value() != 1 ||
		plan.Output.AudioSampleCount.Value() != 480 {
		t.Fatalf("unexpected exact output grids: %+v", plan)
	}
}

func TestSequencePreviewPlanFailsClosedForMissingInputAndFont(t *testing.T) {
	fixture := renderPlanFixture(t)
	fixture.Bindings = fixture.Bindings[:1]
	if _, err := CompileSequencePreviewPlan(fixture); !errors.Is(err, ErrRenderInputRequired) {
		t.Fatalf("missing exact stream proxy: %v", err)
	}

	fixture = renderPlanFixture(t)
	captionID := mustRenderCaptionID(t, "00000000-0000-7000-8000-000000000031")
	start, _ := domain.NewRationalTime(4, 1)
	duration, _ := domain.NewRationalTime(3, 1)
	fixture.Captions = []domain.CaptionState{{
		ID: captionID, Revision: mustRenderRevision(t, 1), SequenceID: fixture.Sequence.ID,
		TrackID: fixture.Sequence.Tracks[2].ID, Range: domain.TimeRange{Start: start, Duration: duration},
		Language: "en", Text: "Pinned captions are executable.",
	}}
	if _, err := CompileSequencePreviewPlan(fixture); !errors.Is(err, ErrRenderFontRequired) {
		t.Fatalf("caption without pinned font: %v", err)
	}
	fixture.FontResource = &domain.RenderFontResource{
		ResourceID: "font:noto-sans-cjk-sc", Version: "fixture-v1", SHA256: renderDigest("f"),
	}
	compiled, err := CompileSequencePreviewPlan(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Plan.Payload.Captions) != 1 || len(compiled.Plan.Payload.FontResources) != 1 ||
		compiled.Plan.Payload.Duration.Value.Value() != 7 ||
		compiled.Plan.Payload.Captions[0].Language != "en" ||
		compiled.Plan.Payload.Captions[0].Style.WrapPolicy != "explicit-lines-clip-v1" ||
		compiled.Plan.Payload.Captions[0].Style.LineHeightBasisPoints != 12_000 ||
		compiled.Plan.Payload.Captions[0].Style.SafeWidthBasisPoint != 9_000 {
		t.Fatalf("caption plan is incomplete: %+v", compiled.Plan.Payload)
	}
}

func TestSequencePreviewPlanReportsEmptyWithoutCreatingSyntheticDuration(t *testing.T) {
	fixture := renderPlanFixture(t)
	fixture.Clips = nil
	fixture.Bindings = nil
	if _, err := CompileSequencePreviewPlan(fixture); !errors.Is(err, ErrRenderSequenceEmpty) {
		t.Fatalf("empty Sequence: %v", err)
	}
}

func renderPlanFixture(t *testing.T) CompileRenderPlanInput {
	t.Helper()
	projectID := mustRenderProjectID(t, "00000000-0000-7000-8000-000000000001")
	sequenceID := mustRenderSequenceID(t, "00000000-0000-7000-8000-000000000002")
	videoTrackID := mustRenderTrackID(t, "00000000-0000-7000-8000-000000000003")
	audioTrackID := mustRenderTrackID(t, "00000000-0000-7000-8000-000000000004")
	captionTrackID := mustRenderTrackID(t, "00000000-0000-7000-8000-000000000005")
	assetID := mustRenderAssetID(t, "00000000-0000-7000-8000-000000000006")
	videoClipID := mustRenderClipID(t, "00000000-0000-7000-8000-000000000007")
	audioClipID := mustRenderClipID(t, "00000000-0000-7000-8000-000000000008")
	groupID := mustRenderLinkGroupID(t, "00000000-0000-7000-8000-000000000009")
	artifactID := mustRenderArtifactID(t, "00000000-0000-7000-8000-000000000010")
	video := proxyVideoStream(t, "00000000-0000-7000-8000-000000000011", 0, []string{"default"})
	audio := proxyAudioStream(t, "00000000-0000-7000-8000-000000000012", 1, []string{"default"})
	fingerprint := renderDigest("a")
	manifest := renderProxyManifest(t, assetID, fingerprint, video, audio)
	canonical, contentDigest, err := domain.CanonicalDigest(
		"open-cut/source-proxy-artifact", SourceProxyArtifactSchema, manifest,
	)
	if err != nil {
		t.Fatal(err)
	}
	total := uint64(len(canonical)) + manifest.Media.ByteSize.Value() + manifest.Video.TimeMap.ByteSize.Value()
	byteSize, _ := domain.NewUInt64(total)
	artifact := domain.ArtifactSummary{
		ID: artifactID, Kind: domain.ArtifactProxy, ProducerVersion: manifest.Producer,
		InputFingerprint: fingerprint, State: domain.ArtifactReady, ByteSize: byteSize,
		ContentDigest: contentDigest, CreatedAt: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC),
	}
	material, err := NewSourceProxyRenderMaterial(manifest)
	if err != nil {
		t.Fatal(err)
	}
	zero, _ := domain.NewRationalTime(0, 1)
	five, _ := domain.NewRationalTime(5, 1)
	rangeFive := domain.TimeRange{Start: zero, Duration: five}
	revision := mustRenderRevision(t, 2)
	return CompileRenderPlanInput{
		ProjectID: projectID, ObservedProjectRevision: mustRenderRevision(t, 3),
		Sequence: domain.Sequence{
			ID: sequenceID, Revision: revision, Name: "main", Role: domain.SequenceRoleMain,
			Format: domain.DefaultSequenceFormat(),
			Tracks: []domain.Track{
				{ID: videoTrackID, Revision: revision, Type: domain.TrackVideo, Label: "Video 1", OrderKey: "a"},
				{ID: audioTrackID, Revision: revision, Type: domain.TrackAudio, Label: "Audio 1", OrderKey: "b"},
				{ID: captionTrackID, Revision: revision, Type: domain.TrackCaption, Label: "Captions 1", OrderKey: "c"},
			},
		},
		Clips: []domain.ClipState{
			{ID: videoClipID, Revision: revision, SequenceID: sequenceID, TrackID: videoTrackID,
				AssetID: assetID, SourceStreamID: video.ID, SourceRange: rangeFive, TimelineRange: rangeFive,
				Enabled: true, LinkGroupID: &groupID},
			{ID: audioClipID, Revision: revision, SequenceID: sequenceID, TrackID: audioTrackID,
				AssetID: assetID, SourceStreamID: audio.ID, SourceRange: rangeFive, TimelineRange: rangeFive,
				Enabled: true, LinkGroupID: &groupID},
		},
		Assets: map[string]RenderAssetSnapshot{assetID.String(): {
			ID: assetID, Revision: revision, AcceptedFingerprint: fingerprint,
		}},
		Bindings: []RenderClipInputBinding{
			{ClipID: videoClipID, Artifact: artifact, Material: material},
			{ClipID: audioClipID, Artifact: artifact, Material: material},
		},
	}
}

func renderExportPlanFixture(t *testing.T) CompileRenderPlanInput {
	t.Helper()
	fixture := renderPlanFixture(t)
	asset := fixture.Assets[fixture.Clips[0].AssetID.String()]
	zero, _ := domain.NewRationalTime(0, 1)
	millisecond, _ := domain.NewRationalTime(1, 1000)
	frameCount, _ := domain.NewUInt64(2)
	audioSamples, _ := domain.NewUInt64(240_000)
	mapSize, _ := domain.NewUInt64(48)
	videoMediaSize, _ := domain.NewUInt64(8192)
	audioMediaSize, _ := domain.NewUInt64(4096)
	videoSource := proxyVideoStream(t, fixture.Clips[0].SourceStreamID.String(), 0, nil)
	audioSource := proxyAudioStream(t, fixture.Clips[1].SourceStreamID.String(), 1, nil)
	videoManifest := RenderInputArtifactManifest{
		AssetID: asset.ID, Fingerprint: asset.AcceptedFingerprint, Profile: RenderInputProfile,
		Producer: "fixture-render-input-v1", SourceEpoch: zero,
		Media: RenderInputArtifactFile{
			Path: "render-input.mkv", MimeType: "video/x-matroska",
			ByteSize: videoMediaSize, SHA256: renderDigest("d"),
		},
		Video: &RenderInputVideoTrack{
			Source: videoSource, SourceStartTime: zero, MaterialStartTime: zero, TimeBase: millisecond,
			Codec: "ffv1", Width: 1920, Height: 1080, PixelFormat: "yuv420p",
			ColorRange: "tv", ColorSpace: "bt709", ColorTransfer: "bt709",
			ColorPrimaries: "bt709", ColorInterpretation: "source-metadata", FrameCount: frameCount,
			TimeMap: RenderInputArtifactFile{
				Path: "video-time-map.bin", MimeType: "application/vnd.open-cut.pts-map",
				ByteSize: mapSize, SHA256: renderDigest("e"),
			},
		},
	}
	audioManifest := RenderInputArtifactManifest{
		AssetID: asset.ID, Fingerprint: asset.AcceptedFingerprint, Profile: RenderInputProfile,
		Producer: "fixture-render-input-v1", SourceEpoch: zero,
		Media: RenderInputArtifactFile{
			Path: "render-input.mkv", MimeType: "video/x-matroska",
			ByteSize: audioMediaSize, SHA256: renderDigest("f"),
		},
		Audio: &RenderInputAudioTrack{
			Source: audioSource, SourceStartTime: zero, MaterialStartTime: zero, TimeBase: millisecond,
			Codec: "pcm_s16le", SampleFormat: "s16", SampleRate: 48000, Channels: 2,
			ChannelLayout: "stereo", ChannelProjection: "stereo-pass-v1",
			DecodedSampleCount: audioSamples,
		},
	}
	videoBinding := renderInputBinding(t, fixture.Clips[0].ID,
		"00000000-0000-7000-8000-000000000020", videoManifest)
	audioBinding := renderInputBinding(t, fixture.Clips[1].ID,
		"00000000-0000-7000-8000-000000000021", audioManifest)
	fixture.Bindings = []RenderClipInputBinding{videoBinding, audioBinding}
	return fixture
}

func renderInputBinding(
	t *testing.T,
	clipID domain.ClipID,
	artifactValue string,
	manifest RenderInputArtifactManifest,
) RenderClipInputBinding {
	t.Helper()
	canonical, digest, err := CanonicalRenderInputArtifactManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	total := uint64(len(canonical)) + manifest.Media.ByteSize.Value()
	if manifest.Video != nil {
		total += manifest.Video.TimeMap.ByteSize.Value()
	}
	byteSize, _ := domain.NewUInt64(total)
	material, err := NewRenderInputRenderMaterial(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return RenderClipInputBinding{
		ClipID: clipID,
		Artifact: domain.ArtifactSummary{
			ID: mustRenderArtifactID(t, artifactValue), Kind: domain.ArtifactRenderInput,
			ProducerVersion: manifest.Producer, InputFingerprint: manifest.Fingerprint,
			State: domain.ArtifactReady, ByteSize: byteSize, ContentDigest: digest,
			CreatedAt: time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
		},
		Material: material,
	}
}

func renderProxyManifest(
	t *testing.T,
	assetID domain.AssetID,
	fingerprint domain.Digest,
	video domain.SourceStream,
	audio domain.SourceStream,
) SourceProxyArtifactManifest {
	t.Helper()
	zero, _ := domain.NewRationalTime(0, 1)
	millisecond, _ := domain.NewRationalTime(1, 1000)
	frameCount, _ := domain.NewUInt64(2)
	audioSampleCount, _ := domain.NewUInt64(240_000)
	timeMapSize, _ := domain.NewUInt64(48)
	mediaSize, _ := domain.NewUInt64(4096)
	return SourceProxyArtifactManifest{
		AssetID: assetID, Fingerprint: fingerprint, Profile: SourceProxyProfile,
		Producer: "fixture-proxy-v1", SourceEpoch: zero,
		Media: SourceProxyArtifactFile{
			Path: "proxy.webm", MimeType: "video/webm", ByteSize: mediaSize, SHA256: renderDigest("b"),
		},
		Video: &SourceProxyVideoTrack{
			Source: video, SourceStartTime: zero, ProxyStartTime: zero, TimeBase: millisecond,
			Codec: "vp9", Width: 1920, Height: 1080, PixelFormat: "yuv420p",
			ColorRange: "tv", ColorSpace: "bt709", ColorTransfer: "bt709", ColorPrimaries: "bt709",
			ColorInterpretation: "assumed-bt709", FrameCount: frameCount,
			TimeMap: SourceProxyArtifactFile{
				Path: "video-time-map.bin", MimeType: "application/vnd.open-cut.pts-map",
				ByteSize: timeMapSize, SHA256: renderDigest("c"),
			},
		},
		Audio: &SourceProxyAudioTrack{
			Source: audio, SourceStartTime: zero, ProxyStartTime: zero, TimeBase: millisecond,
			Codec: "opus", SampleRate: 48000, Channels: 2, ChannelLayout: "stereo",
			ChannelProjection: "stereo-pass-v1", DecodedSampleCount: audioSampleCount,
		},
	}
}

func renderDigest(character string) domain.Digest {
	return domain.Digest("sha256:" + strings.Repeat(character, 64))
}

func mustRenderRevision(t *testing.T, value uint64) domain.Revision {
	t.Helper()
	revision, err := domain.NewRevision(value)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func mustRenderProjectID(t *testing.T, value string) domain.ProjectID {
	t.Helper()
	id, err := domain.ParseProjectID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustRenderSequenceID(t *testing.T, value string) domain.SequenceID {
	t.Helper()
	id, err := domain.ParseSequenceID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustRenderTrackID(t *testing.T, value string) domain.TrackID {
	t.Helper()
	id, err := domain.ParseTrackID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustRenderAssetID(t *testing.T, value string) domain.AssetID {
	t.Helper()
	id, err := domain.ParseAssetID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustRenderClipID(t *testing.T, value string) domain.ClipID {
	t.Helper()
	id, err := domain.ParseClipID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustRenderLinkGroupID(t *testing.T, value string) domain.LinkGroupID {
	t.Helper()
	id, err := domain.ParseLinkGroupID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustRenderArtifactID(t *testing.T, value string) domain.ArtifactID {
	t.Helper()
	id, err := domain.ParseArtifactID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustRenderCaptionID(t *testing.T, value string) domain.CaptionID {
	t.Helper()
	id, err := domain.ParseCaptionID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
