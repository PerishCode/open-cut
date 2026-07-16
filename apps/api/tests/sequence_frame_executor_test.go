package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/internal/mediatoolchain"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/utils/target"
)

type realSequenceFrameRepository struct {
	fixture     string
	media       application.SequencePreviewArtifactFile
	publication *application.CompleteSequenceFrameSet
	failure     *application.FailSequenceFrameSet
}

func (repository *realSequenceFrameRepository) OpenSequencePreviewMedia(
	context.Context,
	domain.ProjectID,
	domain.SequenceID,
	domain.Revision,
	domain.Digest,
	domain.ArtifactID,
) (*os.File, application.SequencePreviewArtifactFile, error) {
	file, err := os.Open(repository.fixture)
	return file, repository.media, err
}

func (*realSequenceFrameRepository) RejectSequencePreviewArtifact(
	context.Context,
	application.RejectSequencePreviewArtifactRecord,
) (application.SequencePreviewJobProjection, error) {
	return application.SequencePreviewJobProjection{}, nil
}

func (repository *realSequenceFrameRepository) CompleteSequenceFrameSet(
	_ context.Context,
	record application.CompleteSequenceFrameSet,
) error {
	repository.publication = &record
	return nil
}

func (repository *realSequenceFrameRepository) FailSequenceFrameSet(
	_ context.Context,
	record application.FailSequenceFrameSet,
) error {
	repository.failure = &record
	return nil
}

func TestRealSequenceFrameExecutorDecodesExactOrdinals(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	closureRoot := filepath.Join(repositoryRoot, "apps", "api", "dist", "sidecar")
	verified, err := mediatoolchain.Load(closureRoot, target.Host())
	if err != nil {
		t.Skipf("built media toolchain unavailable: %v", err)
	}
	frameTool, exists := verified.Capabilities[mediatoolchain.CapabilityFrameRGBV1]
	if !exists {
		t.Skip("built frame decoder capability is unavailable")
	}
	fixture := filepath.Join(t.TempDir(), "canonical.avi")
	if err := mediatoolchain.WriteCanonicalConformanceFixture(fixture); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	fixtureHash := sha256.Sum256(data)
	fixtureDigest := mustServiceRenderID(
		t,
		domain.ParseDigest,
		"sha256:"+hex.EncodeToString(fixtureHash[:]),
	)
	fixtureSize, err := domain.NewUInt64(uint64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	projectID := mustServiceRenderID(t, domain.ParseProjectID, "00000000-0000-7000-8000-000000000101")
	sequenceID := mustServiceRenderID(t, domain.ParseSequenceID, "00000000-0000-7000-8000-000000000102")
	previewJobID := mustServiceRenderID(t, domain.ParseWorkJobID, "00000000-0000-7000-8000-000000000103")
	frameJobID := mustServiceRenderID(t, domain.ParseWorkJobID, "00000000-0000-7000-8000-000000000104")
	attemptID := mustServiceRenderID(t, domain.ParseJobAttemptID, "00000000-0000-7000-8000-000000000105")
	previewArtifactID := mustServiceRenderID(t, domain.ParseArtifactID, "00000000-0000-7000-8000-000000000106")
	revision, _ := domain.NewRevision(1)
	oneSecond, _ := domain.NewRationalTime(1, 1)
	zero, _ := domain.NewRationalTime(0, 1)
	halfSecond, _ := domain.NewRationalTime(1, 2)
	frameRate, _ := domain.NewRationalTime(2, 1)
	videoFrames, _ := domain.NewUInt64(2)
	audioSamples, _ := domain.NewUInt64(domain.SequencePreviewAudioSampleRate)
	renderPlanDigest := mustServiceRenderID(
		t,
		domain.ParseDigest,
		"sha256:"+strings.Repeat("a", 64),
	)
	contentDigest := mustServiceRenderID(
		t,
		domain.ParseDigest,
		"sha256:"+strings.Repeat("b", 64),
	)
	preview := domain.SequencePreviewArtifactSummary{
		ID: previewArtifactID, ProjectID: projectID, SequenceID: sequenceID, SequenceRevision: revision,
		RenderPlanDigest: renderPlanDigest, RendererVersion: "fixture-renderer-v1", RendererTarget: target.Host().String(),
		Profile: domain.SequencePreviewProfileV1, State: domain.SequencePreviewArtifactReady,
		Facts: domain.SequencePreviewMediaFacts{
			SemanticDuration: oneSecond, PresentationDuration: oneSecond,
			CanvasWidth: 16, CanvasHeight: 16, FrameRate: frameRate, VideoFrameCount: videoFrames,
			AudioSampleRate: domain.SequencePreviewAudioSampleRate, AudioSampleCount: audioSamples,
			VideoCodec: "vp9", AudioCodec: "opus", PixelFormat: "yuv420p", ChannelLayout: "stereo",
		},
		ByteSize: fixtureSize, ContentDigest: contentDigest,
	}
	version := verified.Manifest.Version + "@" + frameTool.Entry.SHA256 + "/" + application.SequenceFrameSetProfile
	parameters, err := application.NewSequenceFrameSetParameters(
		projectID, sequenceID, revision, previewJobID, frameRate,
		[]domain.RationalTime{zero, halfSecond}, version,
	)
	if err != nil {
		t.Fatal(err)
	}
	parametersJSON, parametersDigest, err := application.CanonicalSequenceFrameSetParameters(parameters)
	if err != nil {
		t.Fatal(err)
	}
	repository := &realSequenceFrameRepository{
		fixture: fixture,
		media: application.SequencePreviewArtifactFile{
			Path: "preview.webm", MimeType: "video/webm", ByteSize: fixtureSize, SHA256: fixtureDigest,
		},
	}
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	executor, err := service.NewExternalSequenceFrameExecutor(
		repository, frameTool.Entry.Path, version, filepath.Join(t.TempDir(), "attempts"),
		lifecycle.ProfileHarness, application.UUIDv7IdentityGenerator{},
		application.ClockFunc(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatal(err)
	}
	claim := application.WorkJobClaim{
		JobID: frameJobID, AttemptID: attemptID, Kind: domain.WorkJobSequenceFrames,
		ExecutorVersion: version,
		SequenceFrames: &application.SequenceFrameJobClaim{
			ProjectID: projectID, SequenceID: sequenceID, SequenceRevision: revision,
			Parameters: parameters, ParametersDigest: parametersDigest, ParametersJSON: parametersJSON,
			PreviewArtifact: preview,
		},
	}
	if err := executor.Execute(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	if repository.failure != nil || repository.publication == nil {
		t.Fatalf("publication=%+v failure=%+v", repository.publication, repository.failure)
	}
	publication := repository.publication
	if len(publication.PNGs) != 2 || len(publication.Manifest.Samples) != 2 {
		t.Fatalf("publication has %d PNGs and %d samples", len(publication.PNGs), len(publication.Manifest.Samples))
	}
	if bytes.Equal(publication.PNGs[0], publication.PNGs[1]) {
		t.Fatal("exact ordinal decode returned identical frames for ordinals 0 and 1")
	}
	for index, encoded := range publication.PNGs {
		decoded, format, err := image.Decode(bytes.NewReader(encoded))
		if err != nil || format != "png" {
			t.Fatalf("decode PNG %d format=%q err=%v", index, format, err)
		}
		if decoded.Bounds().Dx() != 16 || decoded.Bounds().Dy() != 16 {
			t.Fatalf("PNG %d dimensions are %v", index, decoded.Bounds())
		}
		for y := decoded.Bounds().Min.Y; y < decoded.Bounds().Max.Y; y++ {
			for x := decoded.Bounds().Min.X; x < decoded.Bounds().Max.X; x++ {
				_, _, _, alpha := decoded.At(x, y).RGBA()
				if alpha != 0xffff {
					t.Fatalf("PNG %d contains non-opaque pixel at %d,%d", index, x, y)
				}
			}
		}
	}
}
