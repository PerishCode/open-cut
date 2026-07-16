package mediatoolchain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type rendererConformanceFixture struct {
	ID        string
	Plan      application.PublishedRenderPlan
	Materials renderengine.MaterialPaths
}

type rendererConformanceMaterial struct {
	Root        string
	MediaDigest domain.Digest
	MapDigest   domain.Digest
}

func rendererConformanceFixtures(
	root, mediaPath, fontRoot string,
	font ResourceRecord,
	purpose domain.RenderPlanPurpose,
) ([]rendererConformanceFixture, error) {
	if purpose != domain.RenderPurposeSequencePreview && purpose != domain.RenderPurposeExport {
		return nil, fmt.Errorf("renderer conformance purpose is invalid")
	}
	definitions := []struct {
		id   string
		kind string
		seed int
	}{
		{"av-matrix", "av", 1000},
		{"video-only", "video", 2000},
		{"audio-only", "audio", 3000},
		{"caption-only", "caption", 4000},
	}
	fixtures := make([]rendererConformanceFixture, 0, len(definitions))
	for _, definition := range definitions {
		material := rendererConformanceMaterial{}
		if definition.kind != "caption" {
			var err error
			material, err = stageRendererConformanceMaterial(
				root, mediaPath, definition.id, definition.kind != "audio", purpose,
			)
			if err != nil {
				return nil, err
			}
		}
		fixture, err := buildRendererConformanceFixture(
			definition.id, definition.kind, definition.seed, material, fontRoot, font, purpose,
		)
		if err != nil {
			return nil, fmt.Errorf("build renderer conformance fixture %s: %w", definition.id, err)
		}
		fixtures = append(fixtures, fixture)
	}
	return fixtures, nil
}

func stageRendererConformanceMaterial(
	root, mediaPath, id string,
	withVideo bool,
	purpose domain.RenderPlanPurpose,
) (rendererConformanceMaterial, error) {
	materialRoot := filepath.Join(root, "materials", id)
	if err := os.MkdirAll(materialRoot, 0o700); err != nil {
		return rendererConformanceMaterial{}, err
	}
	materialRoot, err := filepath.EvalSymlinks(materialRoot)
	if err != nil {
		return rendererConformanceMaterial{}, err
	}
	filename := "proxy.webm"
	if purpose == domain.RenderPurposeExport {
		filename = "render-input.mkv"
	}
	stagedMediaPath := filepath.Join(materialRoot, filename)
	if err := copyRegularFile(mediaPath, stagedMediaPath, 0o600); err != nil {
		return rendererConformanceMaterial{}, err
	}
	mediaDigest, _, err := digestFile(stagedMediaPath)
	if err != nil {
		return rendererConformanceMaterial{}, err
	}
	result := rendererConformanceMaterial{Root: materialRoot, MediaDigest: domain.Digest(mediaDigest)}
	if !withVideo {
		return result, nil
	}
	encodedMap, err := application.EncodeSourceProxyTimeMap([]int64{0, 137}, []int64{0, 500})
	if err != nil {
		return rendererConformanceMaterial{}, err
	}
	mapPath := filepath.Join(materialRoot, "video-time-map.bin")
	if err := os.WriteFile(mapPath, encodedMap, 0o600); err != nil {
		return rendererConformanceMaterial{}, err
	}
	result.MapDigest = domain.Digest(digestConformanceBytes(encodedMap))
	return result, nil
}

func buildRendererConformanceFixture(
	id, kind string,
	seed int,
	material rendererConformanceMaterial,
	fontRoot string,
	font ResourceRecord,
	purpose domain.RenderPlanPurpose,
) (rendererConformanceFixture, error) {
	durationNumerator := int64(2)
	if kind == "av" {
		durationNumerator = 6
	}
	duration, err := domain.NewRationalTime(durationNumerator, 7)
	if err != nil {
		return rendererConformanceFixture{}, err
	}
	plan, err := rendererConformanceSkeleton(font, duration, seed, purpose)
	if err != nil {
		return rendererConformanceFixture{}, err
	}
	payload := plan.Plan.Payload
	if kind != "caption" && kind != "av" {
		payload.Captions = []domain.RenderCaptionInstruction{}
		payload.FontResources = []domain.RenderFontResource{}
	}
	materials := renderengine.MaterialPaths{ArtifactRoots: map[string]string{}, Resources: map[string]string{}}
	if kind == "caption" || kind == "av" {
		materials.Resources[font.ID] = fontRoot
	}
	if kind != "caption" {
		input, err := rendererConformanceInput(seed, kind, material, purpose)
		if err != nil {
			return rendererConformanceFixture{}, err
		}
		payload.Inputs = []domain.RenderPlanInput{input}
		materials.ArtifactRoots[input.ArtifactID.String()] = material.Root
		switch kind {
		case "av":
			payload.Video, err = rendererConformanceAVVideo(seed, input)
			if err == nil {
				payload.Audio, err = rendererConformanceAVAudio(seed, input)
			}
			payload.Captions[0].Range = rendererConformanceRange(2, 4)
			payload.Captions[0].Language = "zh-Hans"
			payload.Captions[0].Text = "Open Cut\t שלום\n中文"
		case "video":
			payload.Video, err = rendererConformanceVideoOnly(seed, input)
			payload.Audio = []domain.RenderAudioInstruction{}
		case "audio":
			payload.Video = []domain.RenderVideoInstruction{}
			payload.Audio, err = rendererConformanceAudioOnly(seed, input)
		default:
			err = fmt.Errorf("unsupported conformance fixture kind")
		}
		if err != nil {
			return rendererConformanceFixture{}, err
		}
	}
	_, digest, err := domain.CanonicalDigest("open-cut/render-plan", domain.RenderPlanSchema, payload)
	if err != nil || application.ValidateRenderPlanPayload(payload) != nil {
		return rendererConformanceFixture{}, fmt.Errorf("renderer conformance payload is invalid")
	}
	plan.Plan.Payload, plan.Plan.Digest = payload, digest
	return rendererConformanceFixture{ID: id, Plan: plan, Materials: materials}, nil
}

func rendererConformanceSkeleton(
	font ResourceRecord,
	duration domain.RationalTime,
	seed int,
	purpose domain.RenderPlanPurpose,
) (application.PublishedRenderPlan, error) {
	projectID, err := domain.ParseProjectID(rendererConformanceID(seed + 1))
	if err != nil {
		return application.PublishedRenderPlan{}, err
	}
	sequenceID, err := domain.ParseSequenceID(rendererConformanceID(seed + 2))
	if err != nil {
		return application.PublishedRenderPlan{}, err
	}
	trackID, err := domain.ParseTrackID(rendererConformanceID(seed + 3))
	if err != nil {
		return application.PublishedRenderPlan{}, err
	}
	captionID, err := domain.ParseCaptionID(rendererConformanceID(seed + 4))
	if err != nil {
		return application.PublishedRenderPlan{}, err
	}
	revision, _ := domain.NewRevision(1)
	zero, _ := domain.NewRationalTime(0, 1)
	format := domain.DefaultSequenceFormat()
	format.CanvasWidth, format.CanvasHeight = 160, 90
	input := application.CompileRenderPlanInput{
		ProjectID: projectID, ObservedProjectRevision: revision,
		Sequence: domain.Sequence{
			ID: sequenceID, Revision: revision, Name: "renderer conformance", Role: domain.SequenceRoleMain,
			Format: format,
			Tracks: []domain.Track{{
				ID: trackID, Revision: revision, Type: domain.TrackCaption,
				Label: "Captions", OrderKey: "a",
			}},
		},
		Captions: []domain.CaptionState{{
			ID: captionID, Revision: revision, SequenceID: sequenceID, TrackID: trackID,
			Range: domain.TimeRange{Start: zero, Duration: duration}, Language: "en", Text: "Open Cut",
		}},
		Assets: map[string]application.RenderAssetSnapshot{},
		FontResource: &domain.RenderFontResource{
			ResourceID: font.ID, Version: font.Version, SHA256: domain.Digest(font.SHA256),
		},
	}
	var compiled application.CompiledRenderPlan
	if purpose == domain.RenderPurposeSequencePreview {
		compiled, err = application.CompileSequencePreviewPlan(input)
	} else if purpose == domain.RenderPurposeExport {
		compiled, err = application.CompileSequenceExportPlan(input)
	} else {
		err = fmt.Errorf("renderer conformance purpose is invalid")
	}
	if err != nil {
		return application.PublishedRenderPlan{}, err
	}
	return application.PublishedRenderPlan{Plan: compiled.Plan}, nil
}

func rendererConformanceInput(
	seed int,
	kind string,
	material rendererConformanceMaterial,
	purpose domain.RenderPlanPurpose,
) (domain.RenderPlanInput, error) {
	artifactID, err := domain.ParseArtifactID(rendererConformanceID(seed + 10))
	if err != nil {
		return domain.RenderPlanInput{}, err
	}
	assetID, err := domain.ParseAssetID(rendererConformanceID(seed + 11))
	if err != nil {
		return domain.RenderPlanInput{}, err
	}
	videoStreamID, err := domain.ParseSourceStreamID(rendererConformanceID(seed + 12))
	if err != nil {
		return domain.RenderPlanInput{}, err
	}
	audioStreamID, err := domain.ParseSourceStreamID(rendererConformanceID(seed + 13))
	if err != nil {
		return domain.RenderPlanInput{}, err
	}
	revision, _ := domain.NewRevision(1)
	zero, _ := domain.NewRationalTime(0, 1)
	videoTimeBase, _ := domain.NewRationalTime(1, 1_000)
	audioTimeBase, _ := domain.NewRationalTime(1, 48_000)
	decodedSamples, _ := domain.NewUInt64(48_000)
	profile := application.SourceProxyProfile
	if purpose == domain.RenderPurposeExport {
		profile = application.RenderInputProfile
	}
	input := domain.RenderPlanInput{
		ArtifactID: artifactID, ArtifactDigest: domain.Digest(digestConformanceBytes([]byte(
			material.MediaDigest.String() + "\x00" + material.MapDigest.String(),
		))),
		ProducerVersion: "renderer-conformance-source-v1", Profile: profile,
		AssetID: assetID, AssetRevision: revision,
		Fingerprint: material.MediaDigest, SourceEpoch: zero, MediaDigest: material.MediaDigest,
	}
	if kind != "audio" {
		input.Video = &domain.RenderVideoInput{
			SourceStreamID: videoStreamID, SourceStart: zero, MaterialStart: zero,
			SourceTimeBase: videoTimeBase, MaterialTimeBase: videoTimeBase,
			TimeMapDigest: material.MapDigest, Width: 16, Height: 16,
		}
	}
	if kind != "video" {
		input.Audio = &domain.RenderAudioInput{
			SourceStreamID: audioStreamID, SourceStart: zero, MaterialStart: zero,
			SourceTimeBase: audioTimeBase, MaterialTimeBase: audioTimeBase,
			SampleRate: 48_000, ChannelLayout: "stereo", DecodedSampleCount: decodedSamples,
		}
	}
	return input, nil
}

func rendererConformanceAVVideo(
	seed int,
	input domain.RenderPlanInput,
) ([]domain.RenderVideoInstruction, error) {
	baseTrack, err := domain.ParseTrackID(rendererConformanceID(seed + 20))
	if err != nil {
		return nil, err
	}
	overlayTrack, err := domain.ParseTrackID(rendererConformanceID(seed + 21))
	if err != nil {
		return nil, err
	}
	base := rendererConformancePlacement()
	overlay := rendererConformancePlacement()
	overlay.CropXBasisPoints, overlay.CropYBasisPoints = 1_000, 1_000
	overlay.CropWidthBasisPoints, overlay.CropHeightBasisPoints = 8_000, 8_000
	overlay.ScaleX, _ = domain.NewExactRational(1, 2)
	overlay.ScaleY, _ = domain.NewExactRational(3, 4)
	overlay.TranslateX, _ = domain.NewExactRational(1, 5)
	overlay.TranslateY, _ = domain.NewExactRational(-1, 7)
	overlay.OpacityBasisPoints = 6_000
	overlay.FitPolicy = "cover"
	first, err := rendererConformanceVideoInstruction(seed+30, baseTrack, 0, input, rendererConformanceRange(0, 2), rendererConformanceRange(0, 2), base)
	if err != nil {
		return nil, err
	}
	second, err := rendererConformanceVideoInstruction(seed+31, baseTrack, 0, input, rendererConformanceRange(1, 2), rendererConformanceRange(4, 2), base)
	if err != nil {
		return nil, err
	}
	third, err := rendererConformanceVideoInstruction(seed+32, overlayTrack, 1, input, rendererConformanceRange(0, 4), rendererConformanceRange(1, 4), overlay)
	if err != nil {
		return nil, err
	}
	return []domain.RenderVideoInstruction{first, second, third}, nil
}

func rendererConformanceAVAudio(
	seed int,
	input domain.RenderPlanInput,
) ([]domain.RenderAudioInstruction, error) {
	baseTrack, err := domain.ParseTrackID(rendererConformanceID(seed + 40))
	if err != nil {
		return nil, err
	}
	overlayTrack, err := domain.ParseTrackID(rendererConformanceID(seed + 41))
	if err != nil {
		return nil, err
	}
	first, err := rendererConformanceAudioInstruction(seed+50, baseTrack, 0, input, rendererConformanceRange(0, 3), rendererConformanceRange(0, 3), 0)
	if err != nil {
		return nil, err
	}
	second, err := rendererConformanceAudioInstruction(seed+51, baseTrack, 0, input, rendererConformanceRange(2, 1), rendererConformanceRange(5, 1), 0)
	if err != nil {
		return nil, err
	}
	third, err := rendererConformanceAudioInstruction(seed+52, overlayTrack, 1, input, rendererConformanceRange(0, 4), rendererConformanceRange(1, 4), -6_000)
	if err != nil {
		return nil, err
	}
	return []domain.RenderAudioInstruction{first, second, third}, nil
}

func rendererConformanceVideoOnly(seed int, input domain.RenderPlanInput) ([]domain.RenderVideoInstruction, error) {
	track, err := domain.ParseTrackID(rendererConformanceID(seed + 20))
	if err != nil {
		return nil, err
	}
	instruction, err := rendererConformanceVideoInstruction(
		seed+30, track, 0, input, rendererConformanceRange(0, 2), rendererConformanceRange(0, 2),
		rendererConformancePlacement(),
	)
	return []domain.RenderVideoInstruction{instruction}, err
}

func rendererConformanceAudioOnly(seed int, input domain.RenderPlanInput) ([]domain.RenderAudioInstruction, error) {
	track, err := domain.ParseTrackID(rendererConformanceID(seed + 20))
	if err != nil {
		return nil, err
	}
	instruction, err := rendererConformanceAudioInstruction(
		seed+30, track, 0, input, rendererConformanceRange(0, 2), rendererConformanceRange(0, 2), 6_000,
	)
	return []domain.RenderAudioInstruction{instruction}, err
}

func rendererConformanceVideoInstruction(
	seed int,
	track domain.TrackID,
	layer uint16,
	input domain.RenderPlanInput,
	source, timeline domain.TimeRange,
	placement domain.RenderPlacement,
) (domain.RenderVideoInstruction, error) {
	clip, err := domain.ParseClipID(rendererConformanceID(seed))
	if err != nil {
		return domain.RenderVideoInstruction{}, err
	}
	revision, _ := domain.NewRevision(1)
	return domain.RenderVideoInstruction{
		ClipID: clip, ClipRevision: revision, TrackID: track, TrackRevision: revision, Layer: layer,
		InputArtifactID: input.ArtifactID, SourceStreamID: input.Video.SourceStreamID,
		SourceRange: source, TimelineRange: timeline,
		Orientation: "normalized-by-render-material-v1", Placement: placement,
	}, nil
}

func rendererConformanceAudioInstruction(
	seed int,
	track domain.TrackID,
	layer uint16,
	input domain.RenderPlanInput,
	source, timeline domain.TimeRange,
	gain int32,
) (domain.RenderAudioInstruction, error) {
	clip, err := domain.ParseClipID(rendererConformanceID(seed))
	if err != nil {
		return domain.RenderAudioInstruction{}, err
	}
	revision, _ := domain.NewRevision(1)
	return domain.RenderAudioInstruction{
		ClipID: clip, ClipRevision: revision, TrackID: track, TrackRevision: revision, Layer: layer,
		InputArtifactID: input.ArtifactID, SourceStreamID: input.Audio.SourceStreamID,
		SourceRange: source, TimelineRange: timeline,
		ChannelMapping: "render-material-stereo-v1", GainMilliDB: gain,
	}, nil
}

func rendererConformancePlacement() domain.RenderPlacement {
	one, _ := domain.NewExactRational(1, 1)
	zero, _ := domain.NewExactRational(0, 1)
	return domain.RenderPlacement{
		CropWidthBasisPoints: 10_000, CropHeightBasisPoints: 10_000,
		ScaleX: one, ScaleY: one, TranslateX: zero, TranslateY: zero,
		AnchorXBasisPoints: 5_000, AnchorYBasisPoints: 5_000,
		OpacityBasisPoints: 10_000, FitPolicy: "contain",
	}
}

func rendererConformanceRange(start, duration int64) domain.TimeRange {
	startValue, _ := domain.NewRationalTime(start, 7)
	durationValue, _ := domain.NewRationalTime(duration, 7)
	return domain.TimeRange{Start: startValue, Duration: durationValue}
}

func rendererConformanceID(value int) string {
	return fmt.Sprintf("00000000-0000-7000-8000-%012d", value)
}

func rendererConformanceClosureDigest(
	capabilityID, helperDigest, ffmpegDigest, ffprobeDigest string,
	font ResourceRecord,
) domain.Digest {
	return domain.Digest(digestConformanceBytes([]byte(strings.Join([]string{
		"open-cut/renderer-conformance-execution/v2", capabilityID,
		helperDigest, ffmpegDigest, ffprobeDigest,
		font.ID, font.Version, font.SHA256,
	}, "\x00"))))
}
