package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type sequenceFrameWorkRepository interface {
	OpenSequencePreviewMedia(
		context.Context,
		domain.ProjectID,
		domain.SequenceID,
		domain.Revision,
		domain.Digest,
		domain.ArtifactID,
	) (*os.File, application.SequencePreviewArtifactFile, error)
	RejectSequencePreviewArtifact(
		context.Context,
		application.RejectSequencePreviewArtifactRecord,
	) (application.SequencePreviewJobProjection, error)
	CompleteSequenceFrameSet(context.Context, application.CompleteSequenceFrameSet) error
	FailSequenceFrameSet(context.Context, application.FailSequenceFrameSet) error
}

type ExternalSequenceFrameExecutor struct {
	repository sequenceFrameWorkRepository
	decoder    string
	version    string
	tempRoot   string
	profile    lifecycle.Profile
	identities application.IdentityGenerator
	clock      application.Clock
	wallTime   time.Duration
}

func NewExternalSequenceFrameExecutor(
	repository sequenceFrameWorkRepository,
	decoder string,
	version string,
	tempRoot string,
	profile lifecycle.Profile,
	identities application.IdentityGenerator,
	clock application.Clock,
) (*ExternalSequenceFrameExecutor, error) {
	if repository == nil || !cleanAbsolute(decoder) || version == "" || len(version) > 1024 ||
		!cleanAbsolute(tempRoot) || identities == nil || clock == nil ||
		(profile != lifecycle.ProfileProduction && profile != lifecycle.ProfilePackaged &&
			profile != lifecycle.ProfileDevelopment && profile != lifecycle.ProfileHarness) {
		return nil, fmt.Errorf("sequence frame executor configuration is invalid")
	}
	if info, err := os.Stat(decoder); err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("sequence frame decoder is unavailable")
	}
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create sequence frame attempt root: %w", err)
	}
	return &ExternalSequenceFrameExecutor{
		repository: repository, decoder: decoder, version: version, tempRoot: tempRoot,
		profile: profile, identities: identities, clock: clock, wallTime: 2 * time.Minute,
	}, nil
}

func (executor *ExternalSequenceFrameExecutor) Registration() application.WorkExecutorRegistration {
	return application.WorkExecutorRegistration{Kind: domain.WorkJobSequenceFrames, Version: executor.version}
}

func (executor *ExternalSequenceFrameExecutor) Execute(
	ctx context.Context,
	claim application.WorkJobClaim,
) error {
	if err := executor.validateClaim(claim); err != nil {
		return executor.fail(ctx, claim, "executor-input-invalid", err)
	}
	frame := claim.SequenceFrames
	preview := frame.PreviewArtifact
	file, media, err := executor.repository.OpenSequencePreviewMedia(
		ctx, frame.ProjectID, frame.SequenceID, frame.SequenceRevision,
		preview.RenderPlanDigest, preview.ID,
	)
	if err != nil {
		if errors.Is(err, application.ErrSequencePreviewIntegrity) {
			if rejectErr := executor.rejectPreview(ctx, claim); rejectErr != nil {
				return rejectErr
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return executor.fail(ctx, claim, "input-artifact-unavailable", err)
	}
	defer file.Close()
	if media.Path != "preview.webm" || media.MimeType != "video/webm" ||
		media.ByteSize.Value() == 0 || media.SHA256 == "" {
		return executor.fail(ctx, claim, "input-artifact-invalid", application.ErrSequenceFramesInvalid)
	}
	attemptRoot := filepath.Join(executor.tempRoot, claim.AttemptID.String())
	if !pathWithin(executor.tempRoot, attemptRoot) {
		return executor.fail(ctx, claim, "executor-input-invalid", application.ErrSequenceFramesInvalid)
	}
	if err := os.Mkdir(attemptRoot, 0o700); err != nil {
		return executor.fail(ctx, claim, "attempt-storage-unavailable", err)
	}
	defer os.RemoveAll(attemptRoot)
	executionContext, cancel := context.WithTimeout(ctx, executor.wallTime)
	defer cancel()
	width, height, err := application.SequenceFrameOutputDimensions(
		preview.Facts.CanvasWidth, preview.Facts.CanvasHeight,
	)
	if err != nil {
		return executor.fail(ctx, claim, "frame-profile-invalid", err)
	}
	execution := application.SequenceFrameExecution{
		Samples: make([]application.SequenceFrameExecutionSample, 0, len(frame.Parameters.Samples)),
	}
	total := 0
	for _, coordinate := range frame.Parameters.Samples {
		pngBytes, decodeErr := executor.decodePNG(
			executionContext, attemptRoot, file, coordinate.FrameIndex.Value(), width, height,
		)
		if decodeErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			code := "frame-decode-failed"
			if errors.Is(executionContext.Err(), context.DeadlineExceeded) {
				code = "frame-decode-timeout"
			}
			return executor.fail(ctx, claim, code, decodeErr)
		}
		total += len(pngBytes)
		if total > application.MaximumSequenceFrameArtifactSize {
			return executor.fail(ctx, claim, "frame-output-limit", application.ErrSequenceFramesInvalid)
		}
		execution.Samples = append(execution.Samples, application.SequenceFrameExecutionSample{
			Coordinate: coordinate, Width: width, Height: height, PNG: pngBytes,
		})
	}
	publication, err := executor.materialize(ctx, claim, execution)
	if err != nil {
		return executor.fail(ctx, claim, "frame-output-invalid", err)
	}
	return executor.repository.CompleteSequenceFrameSet(ctx, publication)
}

func (executor *ExternalSequenceFrameExecutor) validateClaim(claim application.WorkJobClaim) error {
	if claim.Kind != domain.WorkJobSequenceFrames || claim.SequenceFrames == nil || claim.Media != nil ||
		claim.SequencePreview != nil || claim.Resource != nil || claim.AttemptID.IsZero() ||
		claim.ExecutorVersion != executor.version || claim.ExecutorTarget != "" ||
		claim.SequenceFrames.Parameters.ExecutorVersion != executor.version ||
		claim.SequenceFrames.Parameters.Validate() != nil {
		return application.ErrSequenceFramesInvalid
	}
	frame := claim.SequenceFrames
	preview := frame.PreviewArtifact
	if preview.ID.IsZero() || preview.State != domain.SequencePreviewArtifactReady ||
		preview.ProjectID != frame.ProjectID || preview.SequenceID != frame.SequenceID ||
		preview.SequenceRevision != frame.SequenceRevision ||
		preview.ID != frame.PreviewArtifact.ID || preview.ContentDigest == "" ||
		preview.RenderPlanDigest == "" || application.ValidateSequencePreviewFacts(preview.Facts) != nil {
		return application.ErrSequenceFramesInvalid
	}
	return nil
}

func (executor *ExternalSequenceFrameExecutor) decodePNG(
	ctx context.Context,
	directory string,
	file *os.File,
	frameIndex uint64,
	width uint32,
	height uint32,
) ([]byte, error) {
	if _, err := file.Seek(0, 0); err != nil {
		return nil, err
	}
	expected := int(width) * int(height) * 3
	stdout := &boundedBuffer{limit: expected}
	stderr := &boundedBuffer{limit: maximumExecutorOutputBytes}
	filter := "select=eq(n\\," + strconv.FormatUint(frameIndex, 10) + "),scale=" +
		strconv.FormatUint(uint64(width), 10) + ":" + strconv.FormatUint(uint64(height), 10) +
		":flags=bilinear,format=rgb24"
	err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: executor.decoder,
		Args: []string{
			"-v", "error", "-hide_banner", "-nostdin", "-protocol_whitelist", "pipe",
			"-i", "pipe:0", "-map", "0:v:0", "-vf", filter, "-frames:v", "1",
			"-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1",
		},
		Directory: directory, Env: executorEnvironment(), Stdin: file, Stdout: stdout, Stderr: stderr,
		Profile: executor.profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 2 * time.Second,
	})
	if err != nil || stdout.exceeded || stderr.exceeded || stdout.Len() != expected {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("exact sequence frame decode failed: %s", strings.TrimSpace(stderr.String()))
	}
	frame := image.NewNRGBA(image.Rect(0, 0, int(width), int(height)))
	rgb := stdout.Bytes()
	for pixel, source := 0, 0; source < len(rgb); pixel, source = pixel+1, source+3 {
		destination := pixel * 4
		frame.Pix[destination] = rgb[source]
		frame.Pix[destination+1] = rgb[source+1]
		frame.Pix[destination+2] = rgb[source+2]
		frame.Pix[destination+3] = 0xff
	}
	output := new(bytes.Buffer)
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(output, frame); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func (executor *ExternalSequenceFrameExecutor) materialize(
	ctx context.Context,
	claim application.WorkJobClaim,
	execution application.SequenceFrameExecution,
) (application.CompleteSequenceFrameSet, error) {
	frame := claim.SequenceFrames
	if len(execution.Samples) != len(frame.Parameters.Samples) {
		return application.CompleteSequenceFrameSet{}, application.ErrSequenceFramesInvalid
	}
	now := executor.clock.Now().UTC()
	artifactID, err := executor.newArtifactID(ctx, now)
	if err != nil {
		return application.CompleteSequenceFrameSet{}, err
	}
	eventID, err := executor.newActivityEventID(ctx, now)
	if err != nil {
		return application.CompleteSequenceFrameSet{}, err
	}
	preview := frame.PreviewArtifact
	manifest := application.SequenceFrameArtifactManifest{
		ProjectID: frame.ProjectID, SequenceID: frame.SequenceID, SequenceRevision: frame.SequenceRevision,
		PreviewJobID: frame.Parameters.PreviewJobID, PreviewArtifactID: preview.ID,
		PreviewArtifactDigest: preview.ContentDigest, RenderPlanDigest: preview.RenderPlanDigest,
		Profile: frame.Parameters.Profile, GridPolicy: frame.Parameters.GridPolicy,
		Producer: executor.version,
		Samples:  make([]application.SequenceFrameArtifactSample, 0, len(execution.Samples)),
	}
	pngs := make([][]byte, 0, len(execution.Samples))
	for index, sample := range execution.Samples {
		hash := sha256.Sum256(sample.PNG)
		digest, err := domain.ParseDigest("sha256:" + hex.EncodeToString(hash[:]))
		if err != nil {
			return application.CompleteSequenceFrameSet{}, err
		}
		byteSize, err := domain.NewUInt64(uint64(len(sample.PNG)))
		if err != nil {
			return application.CompleteSequenceFrameSet{}, err
		}
		manifest.Samples = append(manifest.Samples, application.SequenceFrameArtifactSample{
			SequenceFrameCoordinate: sample.Coordinate, Width: sample.Width, Height: sample.Height,
			Path: fmt.Sprintf("frames/%03d.png", index), ByteSize: byteSize, SHA256: digest,
		})
		pngs = append(pngs, append([]byte(nil), sample.PNG...))
	}
	canonical, digest, err := domain.CanonicalDigest(
		"open-cut/sequence-frame-set-artifact", application.SequenceFrameArtifactSchema, manifest,
	)
	if err != nil || manifest.Validate() != nil {
		return application.CompleteSequenceFrameSet{}, application.ErrSequenceFramesInvalid
	}
	total := uint64(len(canonical))
	for _, data := range pngs {
		total += uint64(len(data))
	}
	byteSize, err := domain.NewUInt64(total)
	if err != nil || total > application.MaximumSequenceFrameArtifactSize {
		return application.CompleteSequenceFrameSet{}, application.ErrSequenceFramesInvalid
	}
	return application.CompleteSequenceFrameSet{
		Claim: claim, ArtifactID: artifactID, Manifest: manifest, ManifestCanonical: canonical,
		ContentDigest: digest, PNGs: pngs, ByteSize: byteSize, EventID: eventID, CompletedAt: now,
	}, nil
}

func (executor *ExternalSequenceFrameExecutor) rejectPreview(
	ctx context.Context,
	claim application.WorkJobClaim,
) error {
	now := executor.clock.Now().UTC()
	retryJobID, err := executor.newWorkJobID(ctx, now)
	if err != nil {
		return err
	}
	eventID, err := executor.newActivityEventID(ctx, now)
	if err != nil {
		return err
	}
	frame := claim.SequenceFrames
	_, err = executor.repository.RejectSequencePreviewArtifact(ctx, application.RejectSequencePreviewArtifactRecord{
		ProjectID: frame.ProjectID, SequenceID: frame.SequenceID, SequenceRevision: frame.SequenceRevision,
		ArtifactID: frame.PreviewArtifact.ID, JobID: frame.Parameters.PreviewJobID,
		RetryJobID: retryJobID, EventID: eventID,
		Code: application.MediaDiagnosticSequenceIntegrityRejected, RejectedAt: now,
	})
	return err
}

func (executor *ExternalSequenceFrameExecutor) fail(
	ctx context.Context,
	claim application.WorkJobClaim,
	code string,
	cause error,
) error {
	if ctx.Err() != nil || errors.Is(cause, application.ErrWorkLeaseLost) {
		return cause
	}
	now := executor.clock.Now().UTC()
	eventID, err := executor.newActivityEventID(ctx, now)
	if err != nil {
		return err
	}
	return executor.repository.FailSequenceFrameSet(ctx, application.FailSequenceFrameSet{
		Claim: claim, Code: code, EventID: eventID, FailedAt: now,
	})
}

func (executor *ExternalSequenceFrameExecutor) newArtifactID(
	ctx context.Context,
	at time.Time,
) (domain.ArtifactID, error) {
	value, err := executor.identities.NewID(ctx, at)
	if err != nil {
		return domain.ArtifactID{}, err
	}
	return domain.ParseArtifactID(value)
}

func (executor *ExternalSequenceFrameExecutor) newWorkJobID(
	ctx context.Context,
	at time.Time,
) (domain.WorkJobID, error) {
	value, err := executor.identities.NewID(ctx, at)
	if err != nil {
		return domain.WorkJobID{}, err
	}
	return domain.ParseWorkJobID(value)
}

func (executor *ExternalSequenceFrameExecutor) newActivityEventID(
	ctx context.Context,
	at time.Time,
) (domain.ActivityEventID, error) {
	value, err := executor.identities.NewID(ctx, at)
	if err != nil {
		return domain.ActivityEventID{}, err
	}
	return domain.ParseActivityEventID(value)
}
