package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

const maximumProbeOutputBytes = 256 << 10

type ExternalMediaProbeExecutor struct {
	access     *SourceAccess
	executable string
	version    string
	tempRoot   string
	profile    lifecycle.Profile
	wallTime   time.Duration
}

func NewExternalMediaProbeExecutor(
	access *SourceAccess,
	executable string,
	version string,
	tempRoot string,
	profile lifecycle.Profile,
) (*ExternalMediaProbeExecutor, error) {
	if access == nil || !cleanAbsolute(executable) || version == "" || len(version) > 256 ||
		!cleanAbsolute(tempRoot) ||
		(profile != lifecycle.ProfileProduction && profile != lifecycle.ProfilePackaged &&
			profile != lifecycle.ProfileDevelopment && profile != lifecycle.ProfileHarness) {
		return nil, fmt.Errorf("media probe executor configuration is invalid")
	}
	if info, err := os.Stat(executable); err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("media probe executor is unavailable")
	}
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create media attempt root: %w", err)
	}
	return &ExternalMediaProbeExecutor{
		access: access, executable: executable, version: version, tempRoot: tempRoot,
		profile: profile, wallTime: 2 * time.Minute,
	}, nil
}

func (executor *ExternalMediaProbeExecutor) Registration() application.MediaExecutorRegistration {
	return application.MediaExecutorRegistration{Kind: domain.MediaJobProbe, Version: executor.version}
}

func (executor *ExternalMediaProbeExecutor) Execute(
	ctx context.Context,
	claim application.MediaJobClaim,
) (application.MediaJobExecution, error) {
	probe, err := executor.Probe(ctx, claim)
	if err != nil {
		return application.MediaJobExecution{}, err
	}
	return application.MediaJobExecution{Probe: &probe}, nil
}

func (executor *ExternalMediaProbeExecutor) Probe(
	ctx context.Context,
	claim application.MediaJobClaim,
) (application.MediaProbe, error) {
	if claim.Kind != domain.MediaJobProbe || claim.AttemptID.IsZero() || claim.AssetID.IsZero() ||
		claim.AcceptedFingerprint == nil {
		return application.MediaProbe{}, application.NewMediaExecutionError(
			"executor-input-invalid", application.ErrMediaSourceRead,
		)
	}
	source, err := executor.access.resolveAssetSource(ctx, claim.AssetID)
	if err != nil {
		return application.MediaProbe{}, err
	}
	if source.Observation != claim.ExpectedObservation {
		return application.MediaProbe{}, application.NewMediaSourceExecutionError(
			"source-observation-changed", domain.AssetChanged, application.ErrMediaSourceMoved,
		)
	}
	attemptRoot := filepath.Join(executor.tempRoot, claim.AttemptID.String())
	if !pathWithin(executor.tempRoot, attemptRoot) {
		return application.MediaProbe{}, application.NewMediaExecutionError(
			"executor-input-invalid", application.ErrMediaSourceRead,
		)
	}
	if err := os.Mkdir(attemptRoot, 0o700); err != nil {
		return application.MediaProbe{}, application.NewMediaExecutionError(
			"attempt-storage-unavailable", err,
		)
	}
	defer os.RemoveAll(attemptRoot)
	executionContext, cancel := context.WithTimeout(ctx, executor.wallTime)
	defer cancel()
	stdout := &boundedBuffer{limit: maximumProbeOutputBytes}
	stderr := &boundedBuffer{limit: maximumExecutorOutputBytes}
	err = lifecycle.Run(executionContext, lifecycle.ProcessSpec{
		Executable: executor.executable,
		Args: []string{
			"-v", "error", "-hide_banner", "-protocol_whitelist", "file",
			"-show_entries", probeShowEntries, "-of", "json=compact=1", source.Path,
		},
		Directory: attemptRoot, Env: executorEnvironment(), Stdout: stdout, Stderr: stderr,
		Profile: executor.profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 2 * time.Second,
	})
	if err != nil || stdout.exceeded || stderr.exceeded {
		if sourceErr := verifyProbeSource(source.Path, source.Observation, *claim.AcceptedFingerprint); sourceErr != nil {
			return application.MediaProbe{}, sourceErr
		}
		code := "probe-failed"
		if errors.Is(executionContext.Err(), context.DeadlineExceeded) {
			code = "probe-timeout"
		}
		return application.MediaProbe{}, application.NewMediaExecutionError(
			code, errors.New("isolated probe executor did not complete"),
		)
	}
	probe, err := DecodeFFProbeOutput(stdout.Bytes())
	if err != nil {
		return application.MediaProbe{}, application.NewMediaExecutionError(
			"probe-output-invalid", err,
		)
	}
	if err := verifyProbeSource(source.Path, source.Observation, *claim.AcceptedFingerprint); err != nil {
		return application.MediaProbe{}, err
	}
	return probe, nil
}

const probeShowEntries = "format=format_name,start_time,duration,bit_rate:" +
	"stream=index,codec_name,codec_type,profile,codec_tag_string,time_base,start_time,duration,duration_ts," +
	"width,height,coded_width,coded_height,sample_aspect_ratio,avg_frame_rate,r_frame_rate,pix_fmt," +
	"color_range,color_space,color_transfer,color_primaries,sample_fmt,sample_rate,channels,channel_layout:" +
	"stream_tags=language:stream_disposition:stream_side_data=rotation"

type ffprobeDocument struct {
	Streams []ffprobeStream `json:"streams"`
	Format  ffprobeFormat   `json:"format"`
}

type ffprobeFormat struct {
	FormatName string `json:"format_name"`
	StartTime  string `json:"start_time"`
	Duration   string `json:"duration"`
	BitRate    string `json:"bit_rate"`
}

type ffprobeStream struct {
	Index             uint32                  `json:"index"`
	CodecName         string                  `json:"codec_name"`
	CodecType         string                  `json:"codec_type"`
	Profile           json.RawMessage         `json:"profile"`
	CodecTag          string                  `json:"codec_tag_string"`
	TimeBase          string                  `json:"time_base"`
	StartTime         string                  `json:"start_time"`
	Duration          string                  `json:"duration"`
	DurationTS        json.RawMessage         `json:"duration_ts"`
	Width             uint32                  `json:"width"`
	Height            uint32                  `json:"height"`
	CodedWidth        uint32                  `json:"coded_width"`
	CodedHeight       uint32                  `json:"coded_height"`
	SampleAspectRatio string                  `json:"sample_aspect_ratio"`
	AverageFrameRate  string                  `json:"avg_frame_rate"`
	NominalFrameRate  string                  `json:"r_frame_rate"`
	PixelFormat       string                  `json:"pix_fmt"`
	ColorRange        string                  `json:"color_range"`
	ColorSpace        string                  `json:"color_space"`
	ColorTransfer     string                  `json:"color_transfer"`
	ColorPrimaries    string                  `json:"color_primaries"`
	SampleFormat      string                  `json:"sample_fmt"`
	SampleRate        string                  `json:"sample_rate"`
	Channels          uint16                  `json:"channels"`
	ChannelLayout     string                  `json:"channel_layout"`
	Tags              map[string]string       `json:"tags"`
	Disposition       map[string]int          `json:"disposition"`
	SideData          []ffprobeStreamSideData `json:"side_data_list"`
}

type ffprobeStreamSideData struct {
	Rotation json.RawMessage `json:"rotation"`
}

// DecodeFFProbeOutput validates and normalizes the closed ffprobe-v1 adapter output.
// It is exported for black-box adapter conformance tests; it is not a product port.
func DecodeFFProbeOutput(data []byte) (application.MediaProbe, error) {
	if len(data) == 0 || len(data) > maximumProbeOutputBytes {
		return application.MediaProbe{}, domain.ErrInvalidMediaFacts
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var document ffprobeDocument
	if err := decoder.Decode(&document); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		len(document.Streams) == 0 || len(document.Streams) > 64 {
		return application.MediaProbe{}, domain.ErrInvalidMediaFacts
	}
	containers := splitContainerNames(document.Format.FormatName)
	if len(containers) == 0 {
		return application.MediaProbe{}, domain.ErrInvalidMediaFacts
	}
	probe := application.MediaProbe{Container: containers[0], ContainerAliases: containers[1:]}
	var err error
	probe.StartTime, err = optionalRational(document.Format.StartTime, false)
	if err != nil {
		return application.MediaProbe{}, err
	}
	probe.Duration, err = optionalRational(document.Format.Duration, true)
	if err != nil {
		return application.MediaProbe{}, err
	}
	if document.Format.BitRate != "" && document.Format.BitRate != "N/A" {
		value, parseErr := strconv.ParseUint(document.Format.BitRate, 10, 63)
		if parseErr != nil {
			return application.MediaProbe{}, domain.ErrInvalidMediaFacts
		}
		bitRate, parseErr := domain.NewUInt64(value)
		if parseErr != nil {
			return application.MediaProbe{}, parseErr
		}
		probe.BitRate = &bitRate
	}
	sort.Slice(document.Streams, func(left, right int) bool { return document.Streams[left].Index < document.Streams[right].Index })
	indexes := make(map[uint32]struct{}, len(document.Streams))
	probe.Streams = make([]domain.SourceStreamDescriptor, 0, len(document.Streams))
	for _, input := range document.Streams {
		if _, duplicate := indexes[input.Index]; duplicate {
			return application.MediaProbe{}, domain.ErrInvalidMediaFacts
		}
		indexes[input.Index] = struct{}{}
		descriptor, parseErr := parseFFProbeStream(input)
		if parseErr != nil {
			return application.MediaProbe{}, parseErr
		}
		probe.Streams = append(probe.Streams, descriptor)
	}
	return probe, nil
}

func parseFFProbeStream(input ffprobeStream) (domain.SourceStreamDescriptor, error) {
	mediaType := parseMediaType(input.CodecType)
	timeBase, err := requiredRational(input.TimeBase, true)
	if err != nil {
		if mediaType != domain.MediaAttachment {
			return domain.SourceStreamDescriptor{}, err
		}
		timeBase, _ = domain.NewRationalTime(1, 1)
	}
	codec := normalizedProbeText(input.CodecName)
	if codec == "" {
		codec = "unknown"
	}
	descriptor := domain.SourceStreamDescriptor{
		Index: input.Index, MediaType: mediaType, Codec: codec,
		CodecProfile: scalarText(input.Profile), CodecTag: normalizedProbeText(input.CodecTag),
		TimeBase: timeBase, Language: normalizedProbeText(input.Tags["language"]),
		Dispositions: trueDispositionNames(input.Disposition),
	}
	descriptor.StartTime, err = optionalRational(input.StartTime, false)
	if err != nil {
		return domain.SourceStreamDescriptor{}, err
	}
	descriptor.Duration, err = streamDuration(input.DurationTS, input.Duration, timeBase)
	if err != nil {
		return domain.SourceStreamDescriptor{}, err
	}
	switch mediaType {
	case domain.MediaVideo:
		rotation, parseErr := parseRotation(input.SideData)
		if parseErr != nil {
			return domain.SourceStreamDescriptor{}, parseErr
		}
		video := &domain.VideoStreamFacts{
			Width: input.Width, Height: input.Height, CodedWidth: input.CodedWidth, CodedHeight: input.CodedHeight,
			Rotation: rotation, PixelFormat: normalizedProbeText(input.PixelFormat),
			ColorRange: normalizedProbeText(input.ColorRange), ColorSpace: normalizedProbeText(input.ColorSpace),
			ColorTransfer:  normalizedProbeText(input.ColorTransfer),
			ColorPrimaries: normalizedProbeText(input.ColorPrimaries),
		}
		video.PixelAspect, err = optionalRational(input.SampleAspectRatio, true)
		if err != nil {
			return domain.SourceStreamDescriptor{}, err
		}
		video.AverageRate, err = optionalRational(input.AverageFrameRate, true)
		if err != nil {
			return domain.SourceStreamDescriptor{}, err
		}
		video.NominalRate, err = optionalRational(input.NominalFrameRate, true)
		if err != nil {
			return domain.SourceStreamDescriptor{}, err
		}
		descriptor.Video = video
	case domain.MediaAudio:
		sampleRate, parseErr := strconv.ParseUint(input.SampleRate, 10, 32)
		if parseErr != nil {
			return domain.SourceStreamDescriptor{}, domain.ErrInvalidMediaFacts
		}
		descriptor.Audio = &domain.AudioStreamFacts{
			SampleFormat: normalizedProbeText(input.SampleFormat), SampleRate: uint32(sampleRate),
			Channels: input.Channels, ChannelLayout: normalizedProbeText(input.ChannelLayout),
		}
	}
	if err := descriptor.Validate(); err != nil {
		return domain.SourceStreamDescriptor{}, err
	}
	return descriptor, nil
}

func requiredRational(value string, positive bool) (domain.RationalTime, error) {
	if value == "" || value == "N/A" || value == "0/0" {
		return domain.RationalTime{}, domain.ErrInvalidMediaFacts
	}
	if strings.Count(value, ":") == 1 && !strings.Contains(value, "/") {
		value = strings.Replace(value, ":", "/", 1)
	}
	rational, ok := new(big.Rat).SetString(value)
	if !ok || rational.Denom().Sign() <= 0 || !rational.Num().IsInt64() || !rational.Denom().IsInt64() ||
		rational.Denom().Int64() > math.MaxInt32 {
		return domain.RationalTime{}, domain.ErrInvalidMediaFacts
	}
	result, err := domain.NewRationalTime(rational.Num().Int64(), int32(rational.Denom().Int64()))
	if err != nil || (positive && !result.IsPositive()) {
		return domain.RationalTime{}, domain.ErrInvalidMediaFacts
	}
	return result, nil
}

func optionalRational(value string, nonNegative bool) (*domain.RationalTime, error) {
	if value == "" || value == "N/A" || value == "0/0" {
		return nil, nil
	}
	parsed, err := requiredRational(value, false)
	if err != nil || (nonNegative && parsed.IsNegative()) {
		return nil, domain.ErrInvalidMediaFacts
	}
	return &parsed, nil
}

func streamDuration(raw json.RawMessage, fallback string, timeBase domain.RationalTime) (*domain.RationalTime, error) {
	text := scalarText(raw)
	if text == "" || text == "N/A" {
		return optionalRational(fallback, true)
	}
	ticks, ok := new(big.Int).SetString(text, 10)
	if !ok {
		return nil, domain.ErrInvalidMediaFacts
	}
	duration := new(big.Rat).SetInt(ticks)
	duration.Mul(duration, new(big.Rat).SetFrac(
		big.NewInt(timeBase.Value.Value()), big.NewInt(int64(timeBase.Scale)),
	))
	if duration.Sign() < 0 || !duration.Num().IsInt64() || !duration.Denom().IsInt64() ||
		duration.Denom().Int64() > math.MaxInt32 {
		return nil, domain.ErrInvalidMediaFacts
	}
	parsed, err := domain.NewRationalTime(duration.Num().Int64(), int32(duration.Denom().Int64()))
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseMediaType(value string) domain.MediaType {
	switch value {
	case "video":
		return domain.MediaVideo
	case "audio":
		return domain.MediaAudio
	case "subtitle":
		return domain.MediaSubtitle
	case "data":
		return domain.MediaData
	case "attachment":
		return domain.MediaAttachment
	default:
		return domain.MediaOther
	}
}

func splitContainerNames(value string) []string {
	result := make([]string, 0, 4)
	seen := make(map[string]struct{})
	for _, item := range strings.Split(value, ",") {
		item = normalizedProbeText(item)
		if item == "" {
			continue
		}
		if _, duplicate := seen[item]; duplicate {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func trueDispositionNames(values map[string]int) []string {
	result := make([]string, 0, len(values))
	for name, enabled := range values {
		name = normalizedProbeText(name)
		if enabled != 0 && name != "" {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result
}

func parseRotation(values []ffprobeStreamSideData) (int16, error) {
	rotation := int64(0)
	found := false
	for _, value := range values {
		text := scalarText(value.Rotation)
		if text == "" {
			continue
		}
		parsed, err := strconv.ParseInt(text, 10, 16)
		if err != nil || found {
			return 0, domain.ErrInvalidMediaFacts
		}
		rotation, found = parsed, true
	}
	normalized := ((rotation % 360) + 360) % 360
	if normalized != 0 && normalized != 90 && normalized != 180 && normalized != 270 {
		return 0, domain.ErrInvalidMediaFacts
	}
	return int16(normalized), nil
}

func scalarText(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return normalizedProbeText(text)
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&number) == nil {
		return normalizedProbeText(number.String())
	}
	return ""
}

func normalizedProbeText(value string) string {
	value = strings.TrimSpace(value)
	if value == "unknown" || value == "N/A" {
		return ""
	}
	return value
}

func verifyProbeSource(path string, expected domain.SourceObservation, fingerprint domain.Digest) error {
	file, err := os.Open(path)
	if err != nil {
		return application.NewMediaSourceExecutionError(
			"source-unreadable", domain.AssetUnreadable, application.ErrMediaSourceRead,
		)
	}
	defer file.Close()
	beforeInfo, err := file.Stat()
	if err != nil || !beforeInfo.Mode().IsRegular() {
		return application.NewMediaSourceExecutionError(
			"source-unreadable", domain.AssetUnreadable, application.ErrMediaSourceRead,
		)
	}
	before, err := sourceObservation(file, beforeInfo)
	if err != nil || before != expected {
		return application.NewMediaSourceExecutionError(
			"source-observation-changed", domain.AssetChanged, application.ErrMediaSourceMoved,
		)
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return application.NewMediaSourceExecutionError(
			"source-unreadable", domain.AssetUnreadable, application.ErrMediaSourceRead,
		)
	}
	afterInfo, err := file.Stat()
	if err != nil {
		return application.NewMediaSourceExecutionError(
			"source-unreadable", domain.AssetUnreadable, application.ErrMediaSourceRead,
		)
	}
	after, err := sourceObservation(file, afterInfo)
	actual := "sha256:" + hex.EncodeToString(digest.Sum(nil))
	if err != nil || after != before || actual != fingerprint.String() {
		return application.NewMediaSourceExecutionError(
			"source-fingerprint-changed", domain.AssetChanged, application.ErrMediaSourceMoved,
		)
	}
	return nil
}
