package application

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/product/rendercontract"
)

const (
	MaximumSourceProxyManifestSize = 1 << 20
	MaximumSourceProxyFrames       = rendercontract.MaximumSourceProxyFrames
	MaximumSourceProxyAudioSamples = rendercontract.MaximumSourceProxySamples
	MaximumSourceProxyArtifactSize = 512 << 30
	sourceProxyTimeMapHeaderSize   = 16
	sourceProxyTimeMapRecordSize   = 16
)

var sourceProxyTimeMapMagic = [8]byte{'O', 'C', 'P', 'M', 'A', 'P', '0', '1'}

type PreparedMediaWorkspace interface {
	Open(relativePath string) (io.ReadCloser, error)
	Release() error
}

type SourceProxyArtifactFile struct {
	Path     string        `json:"path"`
	MimeType string        `json:"mimeType"`
	ByteSize domain.UInt64 `json:"byteSize"`
	SHA256   domain.Digest `json:"sha256"`
}

type SourceProxyVideoTrack struct {
	Source              domain.SourceStream     `json:"source"`
	SourceStartTime     domain.RationalTime     `json:"sourceStartTime"`
	ProxyStartTime      domain.RationalTime     `json:"proxyStartTime"`
	TimeBase            domain.RationalTime     `json:"timeBase"`
	Codec               string                  `json:"codec"`
	Width               uint32                  `json:"width"`
	Height              uint32                  `json:"height"`
	PixelFormat         string                  `json:"pixelFormat"`
	ColorRange          string                  `json:"colorRange"`
	ColorSpace          string                  `json:"colorSpace"`
	ColorTransfer       string                  `json:"colorTransfer"`
	ColorPrimaries      string                  `json:"colorPrimaries"`
	ColorInterpretation string                  `json:"colorInterpretation"`
	FrameCount          domain.UInt64           `json:"frameCount"`
	TimeMap             SourceProxyArtifactFile `json:"timeMap"`
}

type SourceProxyAudioTrack struct {
	Source             domain.SourceStream `json:"source"`
	SourceStartTime    domain.RationalTime `json:"sourceStartTime"`
	ProxyStartTime     domain.RationalTime `json:"proxyStartTime"`
	TimeBase           domain.RationalTime `json:"timeBase"`
	Codec              string              `json:"codec"`
	SampleRate         uint32              `json:"sampleRate"`
	Channels           uint16              `json:"channels"`
	ChannelLayout      string              `json:"channelLayout"`
	ChannelProjection  string              `json:"channelProjection"`
	DecodedSampleCount domain.UInt64       `json:"decodedSampleCount"`
}

type SourceProxyArtifactManifest struct {
	AssetID     domain.AssetID          `json:"assetId"`
	Fingerprint domain.Digest           `json:"fingerprint"`
	Profile     string                  `json:"profile"`
	Producer    string                  `json:"producer"`
	SourceEpoch domain.RationalTime     `json:"sourceEpoch"`
	Media       SourceProxyArtifactFile `json:"media"`
	Video       *SourceProxyVideoTrack  `json:"video,omitempty"`
	Audio       *SourceProxyAudioTrack  `json:"audio,omitempty"`
}

type SourceProxyTimeMapPoint struct {
	SourcePTS int64
	ProxyPTS  int64
}

type MediaProxyExecution struct {
	SourceEpoch domain.RationalTime
	Media       SourceProxyArtifactFile
	Video       *SourceProxyVideoTrack
	Audio       *SourceProxyAudioTrack
	Workspace   PreparedMediaWorkspace
}

type CompleteMediaProxy struct {
	Claim             MediaJobClaim
	ArtifactID        domain.ArtifactID
	Parameters        InitialMediaJobParameters
	Manifest          SourceProxyArtifactManifest
	ManifestCanonical []byte
	ContentDigest     domain.Digest
	ByteSize          domain.UInt64
	Workspace         PreparedMediaWorkspace
	EventID           domain.ActivityEventID
	CompletedAt       time.Time
}

func SelectSourceProxyStreams(
	streams []domain.SourceStream,
	selection SourceProxySelection,
) (*domain.SourceStream, *domain.SourceStream, error) {
	if selection.Validate() != nil {
		return nil, nil, domain.ErrInvalidMediaFacts
	}
	if selection.Policy == SourceProxySelectionExplicit {
		return selectExplicitSourceProxyStreams(streams, selection)
	}
	var video, audio *domain.SourceStream
	for _, stream := range streams {
		if stream.ID.IsZero() || stream.Descriptor.Validate() != nil {
			return nil, nil, domain.ErrInvalidMediaFacts
		}
		candidate := stream
		switch stream.Descriptor.MediaType {
		case domain.MediaVideo:
			if preferredSourceProxyStream(candidate, video) {
				video = &candidate
			}
		case domain.MediaAudio:
			if preferredSourceProxyStream(candidate, audio) {
				audio = &candidate
			}
		}
	}
	if video == nil && audio == nil {
		return nil, nil, domain.ErrInvalidMediaFacts
	}
	return video, audio, nil
}

func selectExplicitSourceProxyStreams(
	streams []domain.SourceStream,
	selection SourceProxySelection,
) (*domain.SourceStream, *domain.SourceStream, error) {
	var video, audio *domain.SourceStream
	for _, stream := range streams {
		if stream.ID.IsZero() || stream.Descriptor.Validate() != nil {
			return nil, nil, domain.ErrInvalidMediaFacts
		}
		candidate := stream
		if selection.VideoStreamID != nil && stream.ID == *selection.VideoStreamID {
			if stream.Descriptor.MediaType != domain.MediaVideo || video != nil {
				return nil, nil, domain.ErrInvalidMediaFacts
			}
			video = &candidate
		}
		if selection.AudioStreamID != nil && stream.ID == *selection.AudioStreamID {
			if stream.Descriptor.MediaType != domain.MediaAudio || audio != nil {
				return nil, nil, domain.ErrInvalidMediaFacts
			}
			audio = &candidate
		}
	}
	if (selection.VideoStreamID != nil && video == nil) ||
		(selection.AudioStreamID != nil && audio == nil) {
		return nil, nil, domain.ErrInvalidMediaFacts
	}
	return video, audio, nil
}

func preferredSourceProxyStream(candidate domain.SourceStream, selected *domain.SourceStream) bool {
	if selected == nil {
		return true
	}
	candidateDefault := slices.Contains(candidate.Descriptor.Dispositions, "default")
	selectedDefault := slices.Contains(selected.Descriptor.Dispositions, "default")
	if candidateDefault != selectedDefault {
		return candidateDefault
	}
	return candidate.Descriptor.Index < selected.Descriptor.Index
}

func (manifest SourceProxyArtifactManifest) Validate() error {
	if manifest.AssetID.IsZero() || manifest.Fingerprint == "" ||
		manifest.Profile != SourceProxyProfile || manifest.Producer == "" || len(manifest.Producer) > 256 ||
		manifest.SourceEpoch.Validate() != nil || (manifest.Video == nil && manifest.Audio == nil) ||
		validateSourceProxyFile(manifest.Media, "proxy.webm", sourceProxyMIME(manifest.Video != nil)) != nil {
		return domain.ErrInvalidMediaFacts
	}
	if _, err := domain.ParseDigest(manifest.Fingerprint.String()); err != nil {
		return domain.ErrInvalidMediaFacts
	}
	if manifest.Video != nil && validateSourceProxyVideo(*manifest.Video, manifest.SourceEpoch) != nil {
		return domain.ErrInvalidMediaFacts
	}
	if manifest.Audio != nil && validateSourceProxyAudio(*manifest.Audio, manifest.SourceEpoch) != nil {
		return domain.ErrInvalidMediaFacts
	}
	return nil
}

func validateSourceProxyVideo(track SourceProxyVideoTrack, epoch domain.RationalTime) error {
	if track.Source.ID.IsZero() || track.Source.Descriptor.Validate() != nil ||
		track.Source.Descriptor.MediaType != domain.MediaVideo || track.Source.Descriptor.Video == nil ||
		track.SourceStartTime.Validate() != nil || track.ProxyStartTime.Validate() != nil ||
		track.ProxyStartTime.IsNegative() || track.TimeBase.Validate() != nil || !track.TimeBase.IsPositive() ||
		track.Codec != "vp9" || track.PixelFormat != "yuv420p" || track.ColorRange != "tv" ||
		track.ColorSpace != "bt709" || track.ColorTransfer != "bt709" || track.ColorPrimaries != "bt709" ||
		track.Width < 2 || track.Height < 2 || track.Width > 1920 || track.Height > 1920 ||
		track.Width%2 != 0 || track.Height%2 != 0 || track.FrameCount.Value() == 0 ||
		(track.ColorInterpretation != "source-metadata" && track.ColorInterpretation != "assumed-bt709") ||
		track.FrameCount.Value() > MaximumSourceProxyFrames ||
		validateSourceProxyFile(track.TimeMap, "video-time-map.bin", "application/vnd.open-cut.pts-map") != nil {
		return domain.ErrInvalidMediaFacts
	}
	expectedSize := uint64(sourceProxyTimeMapHeaderSize) +
		track.FrameCount.Value()*sourceProxyTimeMapRecordSize
	if track.TimeMap.ByteSize.Value() != expectedSize {
		return domain.ErrInvalidMediaFacts
	}
	return validateSourceProxyTrackStart(track.SourceStartTime, track.ProxyStartTime, epoch)
}

func validateSourceProxyAudio(track SourceProxyAudioTrack, epoch domain.RationalTime) error {
	if track.Source.ID.IsZero() || track.Source.Descriptor.Validate() != nil ||
		track.Source.Descriptor.MediaType != domain.MediaAudio || track.Source.Descriptor.Audio == nil ||
		track.SourceStartTime.Validate() != nil || track.ProxyStartTime.Validate() != nil ||
		track.ProxyStartTime.IsNegative() || track.TimeBase.Validate() != nil || !track.TimeBase.IsPositive() ||
		track.Codec != "opus" || track.SampleRate != 48000 || track.Channels != 2 || track.ChannelLayout != "stereo" ||
		track.DecodedSampleCount.Value() == 0 || track.DecodedSampleCount.Value() > MaximumSourceProxyAudioSamples ||
		(track.ChannelProjection != "mono-duplicate-v1" && track.ChannelProjection != "stereo-pass-v1") {
		return domain.ErrInvalidMediaFacts
	}
	return validateSourceProxyTrackStart(track.SourceStartTime, track.ProxyStartTime, epoch)
}

func validateSourceProxyTrackStart(
	sourceStart domain.RationalTime,
	proxyStart domain.RationalTime,
	epoch domain.RationalTime,
) error {
	comparison, err := sourceStart.Compare(epoch)
	if err != nil || comparison < 0 || proxyStart.IsNegative() {
		return domain.ErrInvalidMediaFacts
	}
	return nil
}

func validateSourceProxyFile(file SourceProxyArtifactFile, path, mime string) error {
	if file.Path != path || file.MimeType != mime || file.ByteSize.Value() == 0 {
		return domain.ErrInvalidMediaFacts
	}
	if _, err := domain.ParseDigest(file.SHA256.String()); err != nil {
		return domain.ErrInvalidMediaFacts
	}
	return nil
}

func sourceProxyMIME(hasVideo bool) string {
	if hasVideo {
		return "video/webm"
	}
	return "audio/webm"
}

func DecodeSourceProxyArtifactManifest(data []byte) (SourceProxyArtifactManifest, error) {
	if len(data) == 0 || len(data) > MaximumSourceProxyManifestSize {
		return SourceProxyArtifactManifest{}, domain.ErrInvalidMediaFacts
	}
	var envelope struct {
		Domain  string                      `json:"domain"`
		Payload SourceProxyArtifactManifest `json:"payload"`
		Schema  string                      `json:"schema"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		envelope.Domain != "open-cut/source-proxy-artifact" ||
		envelope.Schema != SourceProxyArtifactSchema || envelope.Payload.Validate() != nil {
		return SourceProxyArtifactManifest{}, domain.ErrInvalidMediaFacts
	}
	return envelope.Payload, nil
}

func EncodeSourceProxyTimeMap(sourcePTS, proxyPTS []int64) ([]byte, error) {
	result := new(bytes.Buffer)
	result.Grow(sourceProxyTimeMapHeaderSize + len(sourcePTS)*sourceProxyTimeMapRecordSize)
	if err := WriteSourceProxyTimeMap(result, sourcePTS, proxyPTS); err != nil {
		return nil, err
	}
	return result.Bytes(), nil
}

func WriteSourceProxyTimeMap(writer io.Writer, sourcePTS, proxyPTS []int64) error {
	if writer == nil || len(sourcePTS) == 0 || len(sourcePTS) != len(proxyPTS) ||
		len(sourcePTS) > MaximumSourceProxyFrames {
		return domain.ErrInvalidMediaFacts
	}
	for index := range sourcePTS {
		if index > 0 && (sourcePTS[index] <= sourcePTS[index-1] || proxyPTS[index] <= proxyPTS[index-1]) {
			return domain.ErrInvalidMediaFacts
		}
	}
	header := make([]byte, sourceProxyTimeMapHeaderSize)
	copy(header[:8], sourceProxyTimeMapMagic[:])
	binary.BigEndian.PutUint64(header[8:16], uint64(len(sourcePTS)))
	if _, err := writer.Write(header); err != nil {
		return err
	}
	record := make([]byte, sourceProxyTimeMapRecordSize)
	for index := range sourcePTS {
		binary.BigEndian.PutUint64(record[:8], uint64(sourcePTS[index]))
		binary.BigEndian.PutUint64(record[8:16], uint64(proxyPTS[index]))
		if _, err := writer.Write(record); err != nil {
			return err
		}
	}
	return nil
}

func ValidateSourceProxyTimeMap(data []byte, expectedCount uint64) error {
	if expectedCount == 0 || expectedCount > MaximumSourceProxyFrames ||
		len(data) != sourceProxyTimeMapHeaderSize+int(expectedCount)*sourceProxyTimeMapRecordSize {
		return domain.ErrInvalidMediaFacts
	}
	return ValidateSourceProxyTimeMapReader(bytes.NewReader(data), expectedCount)
}

func ValidateSourceProxyTimeMapReader(reader io.Reader, expectedCount uint64) error {
	if reader == nil || expectedCount == 0 || expectedCount > MaximumSourceProxyFrames {
		return domain.ErrInvalidMediaFacts
	}
	header := make([]byte, sourceProxyTimeMapHeaderSize)
	if _, err := io.ReadFull(reader, header); err != nil ||
		!bytes.Equal(header[:8], sourceProxyTimeMapMagic[:]) ||
		binary.BigEndian.Uint64(header[8:16]) != expectedCount {
		return domain.ErrInvalidMediaFacts
	}
	var previousSource, previousProxy int64
	record := make([]byte, sourceProxyTimeMapRecordSize)
	for index := uint64(0); index < expectedCount; index++ {
		if _, err := io.ReadFull(reader, record); err != nil {
			return domain.ErrInvalidMediaFacts
		}
		source := int64(binary.BigEndian.Uint64(record[:8]))
		proxy := int64(binary.BigEndian.Uint64(record[8:16]))
		if index > 0 && (source <= previousSource || proxy <= previousProxy) {
			return fmt.Errorf("source proxy time map is not strictly ordered")
		}
		previousSource, previousProxy = source, proxy
	}
	var trailing [1]byte
	if count, err := reader.Read(trailing[:]); count != 0 || err != io.EOF {
		return domain.ErrInvalidMediaFacts
	}
	return nil
}

// ReadSourceProxyTimeMapPointAt performs one bounded random-access read against
// an already integrity-verified OCPMAP01 file. It still verifies the immutable
// header/count binding so a caller can never reinterpret another file shape.
func ReadSourceProxyTimeMapPointAt(
	reader io.ReaderAt,
	expectedCount uint64,
	index uint64,
) (SourceProxyTimeMapPoint, error) {
	if reader == nil || expectedCount == 0 || expectedCount > MaximumSourceProxyFrames || index >= expectedCount {
		return SourceProxyTimeMapPoint{}, domain.ErrInvalidMediaFacts
	}
	header := make([]byte, sourceProxyTimeMapHeaderSize)
	if _, err := reader.ReadAt(header, 0); err != nil ||
		!bytes.Equal(header[:8], sourceProxyTimeMapMagic[:]) ||
		binary.BigEndian.Uint64(header[8:16]) != expectedCount {
		return SourceProxyTimeMapPoint{}, domain.ErrInvalidMediaFacts
	}
	record := make([]byte, sourceProxyTimeMapRecordSize)
	offset := int64(sourceProxyTimeMapHeaderSize) + int64(index)*sourceProxyTimeMapRecordSize
	if _, err := reader.ReadAt(record, offset); err != nil {
		return SourceProxyTimeMapPoint{}, domain.ErrInvalidMediaFacts
	}
	return SourceProxyTimeMapPoint{
		SourcePTS: int64(binary.BigEndian.Uint64(record[:8])),
		ProxyPTS:  int64(binary.BigEndian.Uint64(record[8:16])),
	}, nil
}
