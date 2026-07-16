package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/transcriptadapter"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type TranscriptModelAccess interface {
	Resolve(context.Context, application.MediaJobClaim) (string, error)
}

type ExternalMediaTranscriptExecutor struct {
	access   *SourceAccess
	models   TranscriptModelAccess
	probe    string
	ffmpeg   string
	whisper  string
	version  string
	target   string
	tempRoot string
	profile  lifecycle.Profile
	wallTime time.Duration
}

func NewExternalMediaTranscriptExecutor(
	access *SourceAccess,
	models TranscriptModelAccess,
	probe, ffmpeg, whisper, version, targetName, tempRoot string,
	profile lifecycle.Profile,
) (*ExternalMediaTranscriptExecutor, error) {
	if access == nil || models == nil || !cleanAbsolute(probe) || !cleanAbsolute(ffmpeg) ||
		!cleanAbsolute(whisper) || version == "" || len(version) > 1024 || targetName == "" ||
		len(targetName) > 128 || !cleanAbsolute(tempRoot) || !validLifecycleProfile(profile) {
		return nil, fmt.Errorf("media transcript executor configuration is invalid")
	}
	for _, executable := range []string{probe, ffmpeg, whisper} {
		if info, err := os.Stat(executable); err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("media transcript executor is unavailable")
		}
	}
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create transcript attempt root: %w", err)
	}
	return &ExternalMediaTranscriptExecutor{
		access: access, models: models, probe: probe, ffmpeg: ffmpeg, whisper: whisper,
		version: version, target: targetName, tempRoot: tempRoot, profile: profile, wallTime: 12 * time.Hour,
	}, nil
}

func (executor *ExternalMediaTranscriptExecutor) Registration() application.MediaExecutorRegistration {
	return application.MediaExecutorRegistration{
		Kind: domain.MediaJobTranscript, Version: executor.version, Target: executor.target,
	}
}

func (executor *ExternalMediaTranscriptExecutor) Execute(
	ctx context.Context,
	claim application.MediaJobClaim,
) (application.MediaJobExecution, error) {
	if claim.TranscriptNoAudio {
		if claim.Kind != domain.MediaJobTranscript || claim.TranscriptBinding != nil || claim.SourceStream != nil {
			return application.MediaJobExecution{}, transcriptExecutionError("executor-input-invalid", application.ErrMediaSourceRead)
		}
		return application.MediaJobExecution{TranscriptNoAudio: true}, nil
	}
	parameters, err := application.DecodeInitialMediaJobParameters(claim.ParametersJSON)
	if err != nil || claim.Kind != domain.MediaJobTranscript || claim.AttemptID.IsZero() || claim.AssetID.IsZero() ||
		claim.AcceptedFingerprint == nil || claim.SourceStream == nil || claim.TranscriptBinding == nil ||
		parameters.AssetID != claim.AssetID || parameters.Kind != domain.MediaJobTranscript ||
		parameters.Profile != application.TranscriptProfile || claim.SourceStream.Descriptor.MediaType != domain.MediaAudio ||
		claim.SourceStream.Descriptor.Audio == nil || claim.TranscriptBinding.Validate() != nil ||
		claim.TranscriptBinding.SourceStreamID != claim.SourceStream.ID ||
		claim.TranscriptBinding.Fingerprint != *claim.AcceptedFingerprint ||
		claim.TranscriptBinding.EngineVersion != executor.version || claim.TranscriptBinding.EngineTarget != executor.target {
		return application.MediaJobExecution{}, transcriptExecutionError("executor-input-invalid", application.ErrMediaSourceRead)
	}
	source, err := executor.access.resolveAssetSource(ctx, claim.AssetID)
	if err != nil {
		return application.MediaJobExecution{}, err
	}
	if source.Observation != claim.ExpectedObservation {
		return application.MediaJobExecution{}, application.NewMediaSourceExecutionError(
			"source-observation-changed", domain.AssetChanged, application.ErrMediaSourceMoved,
		)
	}
	model, err := executor.models.Resolve(ctx, claim)
	if err != nil {
		return application.MediaJobExecution{}, err
	}
	attemptRoot := filepath.Join(executor.tempRoot, claim.AttemptID.String())
	if !pathWithin(executor.tempRoot, attemptRoot) {
		return application.MediaJobExecution{}, transcriptExecutionError("executor-input-invalid", application.ErrMediaSourceRead)
	}
	if err := os.Mkdir(attemptRoot, 0o700); err != nil {
		return application.MediaJobExecution{}, transcriptExecutionError("attempt-storage-unavailable", err)
	}
	defer os.RemoveAll(attemptRoot)
	executionContext, cancel := context.WithTimeout(ctx, executor.wallTime)
	defer cancel()
	firstPTS, err := executor.firstAudioPTS(executionContext, attemptRoot, source.Path, claim.SourceStream.Descriptor)
	if err != nil {
		return application.MediaJobExecution{}, executor.failure(ctx, executionContext, source, *claim.AcceptedFingerprint,
			"transcript-time-invalid", err)
	}
	sourceStart, err := frameTime(firstPTS, claim.SourceStream.Descriptor.TimeBase)
	if err != nil {
		return application.MediaJobExecution{}, transcriptExecutionError("transcript-time-invalid", err)
	}
	channelPolicy, filter, err := transcriptChannelFilter(*claim.SourceStream.Descriptor.Audio)
	if err != nil {
		return application.MediaJobExecution{}, transcriptExecutionError("transcript-audio-layout-unsupported", err)
	}
	wavPath := filepath.Join(attemptRoot, "normalized.wav")
	if err := executor.normalizeAudio(
		executionContext, attemptRoot, source.Path, claim.SourceStream.Descriptor.Index, filter, wavPath,
	); err != nil {
		return application.MediaJobExecution{}, executor.failure(ctx, executionContext, source, *claim.AcceptedFingerprint,
			"transcript-normalization-failed", err)
	}
	proof, err := transcriptadapter.InspectWAV(wavPath, sourceStart, channelPolicy)
	if err != nil {
		return application.MediaJobExecution{}, transcriptExecutionError("transcript-normalization-invalid", err)
	}
	resultPrefix := filepath.Join(attemptRoot, "recognition")
	stderr := &boundedBuffer{limit: 256 << 10}
	err = lifecycle.Run(executionContext, lifecycle.ProcessSpec{
		Executable: executor.whisper,
		Args: []string{
			"-m", model, "-f", wavPath, "-l", "auto", "-ojf", "-of", resultPrefix,
			"-np", "-t", "1", "-p", "1", "-ng", "-nf", "-sow",
		},
		Directory: attemptRoot, Env: executorEnvironment(), Stdout: io.Discard, Stderr: stderr,
		Profile: executor.profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 5 * time.Second,
	})
	if err != nil || stderr.exceeded {
		return application.MediaJobExecution{}, executor.failure(ctx, executionContext, source, *claim.AcceptedFingerprint,
			"transcript-engine-failed", errors.New("local transcript engine did not complete"))
	}
	recognition, err := transcriptadapter.DecodeWhisper(resultPrefix+".json", proof)
	if err != nil {
		return application.MediaJobExecution{}, transcriptExecutionError("transcript-output-invalid", err)
	}
	if err := verifyProbeSource(source.Path, source.Observation, *claim.AcceptedFingerprint); err != nil {
		return application.MediaJobExecution{}, err
	}
	return application.MediaJobExecution{Transcript: &recognition}, nil
}

func (executor *ExternalMediaTranscriptExecutor) firstAudioPTS(
	ctx context.Context,
	directory, source string,
	descriptor domain.SourceStreamDescriptor,
) (int64, error) {
	scanContext, stop := context.WithCancel(ctx)
	collector := &proxyPTSCollector{maximum: 1, stop: stop}
	stderr := &boundedBuffer{limit: maximumExecutorOutputBytes}
	err := lifecycle.Run(scanContext, lifecycle.ProcessSpec{
		Executable: executor.probe,
		Args: []string{
			"-v", "error", "-hide_banner", "-protocol_whitelist", "file",
			"-select_streams", strconv.FormatUint(uint64(descriptor.Index), 10),
			"-show_frames", "-show_entries", "frame=best_effort_timestamp", "-of", "csv=p=0", source,
		},
		Directory: directory, Env: executorEnvironment(), Stdout: collector, Stderr: stderr,
		Profile: executor.profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 2 * time.Second,
	})
	stop()
	if finishErr := collector.Finish(); finishErr != nil {
		return 0, finishErr
	}
	if stderr.exceeded || (err != nil && !collector.stopped) {
		return 0, fmt.Errorf("audio timestamp inventory failed")
	}
	return collector.Values()[0], nil
}

func (executor *ExternalMediaTranscriptExecutor) normalizeAudio(
	ctx context.Context,
	directory, source string,
	streamIndex uint32,
	channelFilter, output string,
) error {
	filters := []string{
		channelFilter,
		"asetpts=PTS-STARTPTS",
		"aresample=16000:filter_size=32:phase_shift=10:linear_interp=0:exact_rational=1:async=1:first_pts=0",
		"aformat=sample_fmts=s16:sample_rates=16000:channel_layouts=mono",
	}
	stderr := &boundedBuffer{limit: 256 << 10}
	err := lifecycle.Run(ctx, lifecycle.ProcessSpec{
		Executable: executor.ffmpeg,
		Args: []string{
			"-v", "error", "-hide_banner", "-nostdin", "-cpuflags", "0",
			"-protocol_whitelist", "file,pipe,fd", "-i", source,
			"-map", "0:" + strconv.FormatUint(uint64(streamIndex), 10), "-vn", "-sn", "-dn",
			"-af", strings.Join(filters, ","), "-c:a", "pcm_s16le", "-ar", "16000", "-ac", "1",
			"-map_metadata", "-1", "-map_chapters", "-1", "-fflags", "+bitexact", "-flags:a", "+bitexact",
			"-f", "wav", "-y", output,
		},
		Directory: directory, Env: executorEnvironment(), Stdout: io.Discard, Stderr: stderr,
		Profile: executor.profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 5 * time.Second,
	})
	if err != nil || stderr.exceeded {
		return fmt.Errorf("canonical PCM normalization failed")
	}
	return nil
}

func (executor *ExternalMediaTranscriptExecutor) failure(
	parent, execution context.Context,
	source resolvedAssetSource,
	fingerprint domain.Digest,
	code string,
	cause error,
) error {
	if sourceErr := verifyProbeSource(source.Path, source.Observation, fingerprint); sourceErr != nil {
		return sourceErr
	}
	if parent.Err() != nil {
		return parent.Err()
	}
	if errors.Is(execution.Err(), context.DeadlineExceeded) {
		code = "transcript-executor-timeout"
	}
	return transcriptExecutionError(code, cause)
}

func transcriptChannelFilter(facts domain.AudioStreamFacts) (string, string, error) {
	switch facts.Channels {
	case 1:
		return "mono-pass-v1", "pan=mono|c0=c0", nil
	case 2:
		return "stereo-equal-v1", "pan=mono|c0=0.5*c0+0.5*c1", nil
	default:
		return "", "", fmt.Errorf("v1 transcript normalization requires mono or stereo audio")
	}
}

func transcriptExecutionError(code string, cause error) error {
	return application.NewMediaExecutionError(code, cause)
}

func validLifecycleProfile(profile lifecycle.Profile) bool {
	return profile == lifecycle.ProfileProduction || profile == lifecycle.ProfilePackaged ||
		profile == lifecycle.ProfileDevelopment || profile == lifecycle.ProfileHarness
}
