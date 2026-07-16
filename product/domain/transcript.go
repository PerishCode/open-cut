package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	TranscriptArtifactSchema          = "open-cut/transcript-artifact/v1"
	TranscriptBindingSchema           = "open-cut/transcript-binding/v1"
	TranscriptSelectionDefaultV1      = "default-audio-v1"
	TranscriptNormalizationV1         = "pcm-s16-mono-16000-source-time-v1"
	TranscriptLanguageAutoOriginal    = "auto-original-v1"
	MaximumTranscriptSegments         = 100_000
	MaximumTranscriptTokens           = 1_000_000
	MaximumTranscriptTokensPerSegment = 2_048
	MaximumTranscriptTextBytes        = 64 << 20
	MaximumTranscriptSegmentBytes     = 8 << 10
	MaximumTranscriptTokenBytes       = 512
	MaximumTranscriptSamples          = 8_000_000_000
	TranscriptSampleRate              = 16_000
)

var ErrInvalidTranscript = errors.New("transcript artifact is invalid")

type TranscriptBinding struct {
	Schema                 string         `json:"schema" enum:"open-cut/transcript-binding/v1"`
	AssetID                AssetID        `json:"assetId"`
	Fingerprint            Digest         `json:"fingerprint" format:"sha256-digest"`
	SourceStreamID         SourceStreamID `json:"sourceStreamId"`
	SourceDescriptorDigest Digest         `json:"sourceDescriptorDigest" format:"sha256-digest"`
	SelectionPolicy        string         `json:"selectionPolicy" enum:"default-audio-v1"`
	NormalizationPolicy    string         `json:"normalizationPolicy" enum:"pcm-s16-mono-16000-source-time-v1"`
	LanguagePolicy         string         `json:"languagePolicy" enum:"auto-original-v1"`
	EngineVersion          string         `json:"engineVersion" minLength:"1" maxLength:"1024"`
	EngineTarget           string         `json:"engineTarget" minLength:"1" maxLength:"128"`
	ModelResourceID        ResourceID     `json:"modelResourceId"`
	ModelName              string         `json:"modelName" pattern:"^[a-z][a-z0-9.-]{0,127}$"`
	ModelVersion           string         `json:"modelVersion" minLength:"1" maxLength:"128"`
	ModelEntryDigest       Digest         `json:"modelEntryDigest" format:"sha256-digest"`
	ModelContentDigest     Digest         `json:"modelContentDigest" format:"sha256-digest"`
}

func (binding TranscriptBinding) Validate() error {
	if binding.Schema != TranscriptBindingSchema || binding.AssetID.IsZero() || binding.SourceStreamID.IsZero() ||
		binding.ModelResourceID.IsZero() || binding.SelectionPolicy != TranscriptSelectionDefaultV1 ||
		binding.NormalizationPolicy != TranscriptNormalizationV1 ||
		binding.LanguagePolicy != TranscriptLanguageAutoOriginal ||
		!validTranscriptIdentity(binding.EngineVersion, 1024) ||
		!validTranscriptIdentity(binding.EngineTarget, 128) ||
		!ValidProductResourceName(binding.ModelName) || !validTranscriptIdentity(binding.ModelVersion, 128) {
		return ErrInvalidTranscript
	}
	for _, digest := range []Digest{
		binding.Fingerprint, binding.SourceDescriptorDigest, binding.ModelEntryDigest, binding.ModelContentDigest,
	} {
		if _, err := ParseDigest(digest.String()); err != nil {
			return ErrInvalidTranscript
		}
	}
	return nil
}

type TranscriptNormalizationProof struct {
	SourceStartTime RationalTime `json:"sourceStartTime"`
	SampleRate      uint32       `json:"sampleRate" enum:"16000"`
	Channels        uint16       `json:"channels" enum:"1"`
	SampleFormat    string       `json:"sampleFormat" enum:"s16le"`
	SampleCount     UInt64       `json:"sampleCount" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	PCMByteSize     UInt64       `json:"pcmByteSize" format:"uint64-decimal" pattern:"^(0|[1-9][0-9]*)$"`
	PCMDigest       Digest       `json:"pcmDigest" format:"sha256-digest"`
	ChannelPolicy   string       `json:"channelPolicy" enum:"mono-pass-v1,stereo-equal-v1"`
	TimingPolicy    string       `json:"timingPolicy" enum:"audio-frame-pts-gap-fill-v1"`
}

func (proof TranscriptNormalizationProof) Validate() error {
	if proof.SourceStartTime.Validate() != nil || proof.SampleRate != TranscriptSampleRate || proof.Channels != 1 ||
		proof.SampleFormat != "s16le" || proof.SampleCount.Value() == 0 ||
		proof.SampleCount.Value() > MaximumTranscriptSamples || proof.PCMByteSize.Value() != proof.SampleCount.Value()*2 ||
		(proof.ChannelPolicy != "mono-pass-v1" && proof.ChannelPolicy != "stereo-equal-v1") ||
		proof.TimingPolicy != "audio-frame-pts-gap-fill-v1" {
		return ErrInvalidTranscript
	}
	if _, err := ParseDigest(proof.PCMDigest.String()); err != nil {
		return ErrInvalidTranscript
	}
	return nil
}

type TranscriptToken struct {
	ID                    TranscriptTokenID `json:"id"`
	Ordinal               uint32            `json:"ordinal"`
	SourceRange           TimeRange         `json:"sourceRange"`
	Text                  string            `json:"text" minLength:"1" maxLength:"512"`
	ConfidenceBasisPoints *uint16           `json:"confidenceBasisPoints,omitempty" minimum:"0" maximum:"10000"`
}

type TranscriptSegment struct {
	ID          TranscriptSegmentID `json:"id"`
	Ordinal     uint32              `json:"ordinal"`
	SourceRange TimeRange           `json:"sourceRange"`
	Text        string              `json:"text" minLength:"1" maxLength:"8192"`
	Tokens      []TranscriptToken   `json:"tokens" maxItems:"2048" nullable:"false"`
}

type TranscriptArtifact struct {
	Schema                        string                       `json:"schema" enum:"open-cut/transcript-artifact/v1"`
	ID                            ArtifactID                   `json:"id"`
	ProjectID                     ProjectID                    `json:"projectId"`
	Binding                       TranscriptBinding            `json:"binding"`
	BindingDigest                 Digest                       `json:"bindingDigest" format:"sha256-digest"`
	DetectedLanguage              string                       `json:"detectedLanguage" maxLength:"64"`
	LanguageConfidenceBasisPoints *uint16                      `json:"languageConfidenceBasisPoints,omitempty" minimum:"0" maximum:"10000"`
	Normalization                 TranscriptNormalizationProof `json:"normalization"`
	Segments                      []TranscriptSegment          `json:"segments" maxItems:"100000" nullable:"false"`
}

func (artifact TranscriptArtifact) Validate() error {
	if artifact.Schema != TranscriptArtifactSchema || artifact.ID.IsZero() || artifact.ProjectID.IsZero() ||
		artifact.Binding.Validate() != nil || artifact.Normalization.Validate() != nil {
		return ErrInvalidTranscript
	}
	_, bindingDigest, err := CanonicalDigest("open-cut/transcript-binding", TranscriptBindingSchema, artifact.Binding)
	if err != nil || bindingDigest != artifact.BindingDigest || len(artifact.Segments) > MaximumTranscriptSegments {
		return ErrInvalidTranscript
	}
	if _, err := ParseCaptionLanguage(artifact.DetectedLanguage); err != nil {
		return ErrInvalidTranscript
	}
	if artifact.LanguageConfidenceBasisPoints != nil && *artifact.LanguageConfidenceBasisPoints > 10_000 {
		return ErrInvalidTranscript
	}
	segmentIDs := make(map[string]struct{}, len(artifact.Segments))
	tokenIDs := make(map[string]struct{})
	normalizedDuration, err := NewRationalTime(int64(artifact.Normalization.SampleCount.Value()), TranscriptSampleRate)
	if err != nil {
		return ErrInvalidTranscript
	}
	normalizedEnd, err := artifact.Normalization.SourceStartTime.Add(normalizedDuration)
	if err != nil {
		return ErrInvalidTranscript
	}
	var previousEnd *RationalTime
	tokenCount, textBytes := 0, 0
	for index, segment := range artifact.Segments {
		if segment.ID.IsZero() || segment.Ordinal != uint32(index) ||
			!validTranscriptText(segment.Text, MaximumTranscriptSegmentBytes, true) ||
			validatePositiveTranscriptRange(segment.SourceRange) != nil || len(segment.Tokens) == 0 ||
			len(segment.Tokens) > MaximumTranscriptTokensPerSegment {
			return ErrInvalidTranscript
		}
		if _, duplicate := segmentIDs[segment.ID.String()]; duplicate {
			return ErrInvalidTranscript
		}
		segmentIDs[segment.ID.String()] = struct{}{}
		if previousEnd != nil {
			comparison, compareErr := segment.SourceRange.Start.Compare(*previousEnd)
			if compareErr != nil || comparison < 0 {
				return ErrInvalidTranscript
			}
		}
		end, _ := segment.SourceRange.End()
		startComparison, startErr := segment.SourceRange.Start.Compare(artifact.Normalization.SourceStartTime)
		endComparison, endErr := end.Compare(normalizedEnd)
		if startErr != nil || endErr != nil || startComparison < 0 || endComparison > 0 {
			return ErrInvalidTranscript
		}
		previousEnd = &end
		var tokenText strings.Builder
		var previousTokenEnd *RationalTime
		for tokenIndex, token := range segment.Tokens {
			if token.ID.IsZero() || token.Ordinal != uint32(tokenIndex) ||
				!validTranscriptText(token.Text, MaximumTranscriptTokenBytes, false) ||
				validatePositiveTranscriptRange(token.SourceRange) != nil ||
				(token.ConfidenceBasisPoints != nil && *token.ConfidenceBasisPoints > 10_000) {
				return ErrInvalidTranscript
			}
			if _, duplicate := tokenIDs[token.ID.String()]; duplicate {
				return ErrInvalidTranscript
			}
			tokenIDs[token.ID.String()] = struct{}{}
			if !rangeContains(segment.SourceRange, token.SourceRange) {
				return ErrInvalidTranscript
			}
			if previousTokenEnd != nil {
				comparison, compareErr := token.SourceRange.Start.Compare(*previousTokenEnd)
				if compareErr != nil || comparison < 0 {
					return ErrInvalidTranscript
				}
			}
			tokenEnd, _ := token.SourceRange.End()
			previousTokenEnd = &tokenEnd
			tokenText.WriteString(token.Text)
			textBytes += len([]byte(token.Text))
			tokenCount++
		}
		if tokenText.String() != segment.Text {
			return ErrInvalidTranscript
		}
		textBytes += len([]byte(segment.Text))
	}
	if tokenCount > MaximumTranscriptTokens || textBytes > MaximumTranscriptTextBytes {
		return ErrInvalidTranscript
	}
	return nil
}

func validatePositiveTranscriptRange(value TimeRange) error {
	if _, err := NewTimeRange(value.Start, value.Duration); err != nil || !value.Duration.IsPositive() {
		return ErrInvalidTranscript
	}
	return nil
}

func rangeContains(parent, child TimeRange) bool {
	parentEnd, parentErr := parent.End()
	childEnd, childErr := child.End()
	startComparison, startErr := child.Start.Compare(parent.Start)
	endComparison, endErr := childEnd.Compare(parentEnd)
	return parentErr == nil && childErr == nil && startErr == nil && endErr == nil &&
		startComparison >= 0 && endComparison <= 0
}

func validTranscriptIdentity(value string, maximum int) bool {
	return value != "" && len([]byte(value)) <= maximum && utf8.ValidString(value) && strings.TrimSpace(value) == value
}

func validTranscriptText(value string, maximum int, trim bool) bool {
	if value == "" || len([]byte(value)) > maximum || !utf8.ValidString(value) || (trim && strings.TrimSpace(value) != value) {
		return false
	}
	for _, current := range value {
		if current < 0x20 || current == 0x7f {
			return false
		}
	}
	return true
}
