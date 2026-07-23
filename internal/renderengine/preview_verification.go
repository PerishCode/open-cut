package renderengine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/product/rendercontract"
)

type SequencePreviewProbeDocument struct {
	Format       SequencePreviewProbeFormat   `json:"format"`
	Streams      []SequencePreviewProbeStream `json:"streams"`
	Programs     []json.RawMessage            `json:"programs"`
	StreamGroups []json.RawMessage            `json:"stream_groups"`
}

type SequencePreviewProbeFormat struct {
	FormatName string `json:"format_name"`
}

type SequencePreviewProbeStream struct {
	Index            uint32 `json:"index"`
	CodecName        string `json:"codec_name"`
	CodecType        string `json:"codec_type"`
	Width            uint32 `json:"width"`
	Height           uint32 `json:"height"`
	AverageFrameRate string `json:"avg_frame_rate"`
	PixelFormat      string `json:"pix_fmt"`
	ColorRange       string `json:"color_range"`
	ColorSpace       string `json:"color_space"`
	ColorTransfer    string `json:"color_transfer"`
	ColorPrimaries   string `json:"color_primaries"`
	SampleRate       string `json:"sample_rate"`
	Channels         uint16 `json:"channels"`
	ChannelLayout    string `json:"channel_layout"`
	ReadFrameCount   string `json:"nb_read_frames"`
}

func ValidateSequencePreviewProbeDocument(
	document SequencePreviewProbeDocument,
	expected domain.SequencePreviewMediaFacts,
) error {
	return ValidateRenderedMediaProbeDocument(document, expected)
}

func ValidateRenderedMediaProbeDocument(
	document SequencePreviewProbeDocument,
	expected domain.RenderedMediaFacts,
) error {
	if len(document.Programs) != 0 || len(document.StreamGroups) != 0 {
		return fmt.Errorf("preview contains an unsupported program or stream group")
	}
	container := false
	for _, alias := range strings.Split(document.Format.FormatName, ",") {
		if alias == "webm" || alias == "matroska" {
			container = true
		}
	}
	if !container || len(document.Streams) != 2 {
		return fmt.Errorf("preview container or stream count is invalid")
	}
	video, audio := false, false
	for _, stream := range document.Streams {
		switch stream.CodecType {
		case "video":
			frames, err := strconv.ParseUint(stream.ReadFrameCount, 10, 64)
			rate, rateErr := requiredPositiveRational(stream.AverageFrameRate)
			if video || err != nil || rateErr != nil || stream.CodecName != expected.VideoCodec ||
				stream.Width != expected.CanvasWidth || stream.Height != expected.CanvasHeight ||
				rate != expected.FrameRate || frames != expected.VideoFrameCount.Value() ||
				stream.PixelFormat != expected.PixelFormat || stream.ColorRange != "tv" ||
				stream.ColorSpace != "bt709" || stream.ColorTransfer != "bt709" ||
				stream.ColorPrimaries != "bt709" {
				return fmt.Errorf("preview video facts are invalid")
			}
			video = true
		case "audio":
			sampleRate, err := strconv.ParseUint(stream.SampleRate, 10, 32)
			if audio || err != nil || stream.CodecName != expected.AudioCodec ||
				uint32(sampleRate) != expected.AudioSampleRate || stream.Channels != 2 ||
				stream.ChannelLayout != expected.ChannelLayout {
				return fmt.Errorf("preview audio facts are invalid")
			}
			audio = true
		default:
			return fmt.Errorf("preview contains an unsupported stream")
		}
	}
	if !video || !audio {
		return fmt.Errorf("preview stream shape is incomplete")
	}
	return nil
}

func requiredPositiveRational(value string) (domain.RationalTime, error) {
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
	if err != nil || !result.IsPositive() {
		return domain.RationalTime{}, domain.ErrInvalidMediaFacts
	}
	return result, nil
}

type AudioSampleCollector struct {
	buffer  []byte
	samples uint64
	written int
	limit   int
	err     error
}

func NewAudioSampleCollector(limit int) *AudioSampleCollector {
	return &AudioSampleCollector{limit: limit}
}

func (collector *AudioSampleCollector) Write(value []byte) (int, error) {
	if collector.err != nil {
		return len(value), nil
	}
	collector.written += len(value)
	if collector.limit <= 0 || collector.written > collector.limit {
		collector.err = fmt.Errorf("preview audio sample report exceeded its bound")
		return len(value), nil
	}
	collector.buffer = append(collector.buffer, value...)
	for {
		index := bytes.IndexByte(collector.buffer, '\n')
		if index < 0 {
			break
		}
		collector.consume(collector.buffer[:index])
		collector.buffer = collector.buffer[index+1:]
	}
	if len(collector.buffer) > 64 {
		collector.err = fmt.Errorf("preview audio sample report line is invalid")
	}
	return len(value), nil
}

func (collector *AudioSampleCollector) consume(value []byte) {
	if collector.err != nil {
		return
	}
	text := strings.TrimSpace(strings.TrimSuffix(string(value), "\r"))
	if text == "" {
		return
	}
	count, err := strconv.ParseUint(text, 10, 32)
	if err != nil || count == 0 || count > rendercontract.MaximumPreviewAudioSamples ||
		collector.samples > rendercontract.MaximumPreviewAudioSamples-count {
		collector.err = fmt.Errorf("preview audio sample report is invalid")
		return
	}
	collector.samples += count
}

func (collector *AudioSampleCollector) Finish() (uint64, error) {
	if collector.err != nil {
		return 0, collector.err
	}
	if len(collector.buffer) > 0 {
		collector.consume(collector.buffer)
		collector.buffer = nil
	}
	if collector.err != nil || collector.samples == 0 {
		return 0, fmt.Errorf("preview audio sample report is empty or invalid")
	}
	return collector.samples, nil
}
