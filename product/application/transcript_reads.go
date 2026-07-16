package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	TranscriptReadSchema          = "open-cut/transcript-read/v1"
	DefaultTranscriptSegmentLimit = uint16(20)
	MaximumTranscriptSegmentLimit = uint16(50)
	MaximumTranscriptReadTokens   = 2_048
)

var (
	ErrTranscriptReadInvalid = errors.New("transcript read is invalid")
	ErrTranscriptNotFound    = errors.New("transcript artifact was not found")
)

type TranscriptReadQuery struct {
	ProjectID  domain.ProjectID
	AssetID    domain.AssetID
	ArtifactID *domain.ArtifactID
	After      string
	Limit      uint16
}

type TranscriptArtifactView struct {
	ID                            domain.ArtifactID     `json:"id"`
	AssetID                       domain.AssetID        `json:"assetId"`
	SourceStreamID                domain.SourceStreamID `json:"sourceStreamId"`
	RecognitionProfile            string                `json:"recognitionProfile" enum:"whisper-small-multilingual-v1"`
	EngineVersion                 string                `json:"engineVersion" minLength:"1" maxLength:"1024"`
	EngineTarget                  string                `json:"engineTarget" minLength:"1" maxLength:"128"`
	ModelName                     string                `json:"modelName" pattern:"^[a-z][a-z0-9.-]{0,127}$"`
	ModelVersion                  string                `json:"modelVersion" minLength:"1" maxLength:"128"`
	DetectedLanguage              string                `json:"detectedLanguage" maxLength:"64"`
	LanguageConfidenceBasisPoints *uint16               `json:"languageConfidenceBasisPoints,omitempty" minimum:"0" maximum:"10000"`
	SourceStartTime               domain.RationalTime   `json:"sourceStartTime"`
	NormalizedSampleCount         domain.UInt64         `json:"normalizedSampleCount" format:"uint64-decimal"`
	IsDefault                     bool                  `json:"isDefault"`
	CreatedAt                     time.Time             `json:"createdAt"`
}

type TranscriptTokenView struct {
	ID                    domain.TranscriptTokenID `json:"id"`
	SourceRange           domain.TimeRange         `json:"sourceRange"`
	Text                  string                   `json:"text" minLength:"1" maxLength:"512"`
	ConfidenceBasisPoints *uint16                  `json:"confidenceBasisPoints,omitempty" minimum:"0" maximum:"10000"`
}

type TranscriptSegmentView struct {
	ID          domain.TranscriptSegmentID `json:"id"`
	Ordinal     uint32                     `json:"ordinal"`
	SourceRange domain.TimeRange           `json:"sourceRange"`
	Text        string                     `json:"text" minLength:"1" maxLength:"8192"`
	Tokens      []TranscriptTokenView      `json:"tokens" maxItems:"2048" nullable:"false"`
}

type TranscriptCorrectionView struct {
	ID            domain.TranscriptCorrectionID `json:"id"`
	Revision      domain.Revision               `json:"revision" format:"uint64-decimal"`
	SegmentIDs    []domain.TranscriptSegmentID  `json:"segmentIds" minItems:"1" maxItems:"256" nullable:"false"`
	SourceRange   domain.TimeRange              `json:"sourceRange"`
	OriginalText  string                        `json:"originalText" minLength:"1" maxLength:"262144"`
	EffectiveText string                        `json:"effectiveText" minLength:"1" maxLength:"262144"`
	Language      domain.CaptionLanguage        `json:"language" maxLength:"64"`
}

type TranscriptReadPage struct {
	Schema         string                     `json:"schema" enum:"open-cut/transcript-read/v1"`
	Artifact       TranscriptArtifactView     `json:"artifact"`
	Segments       []TranscriptSegmentView    `json:"segments" maxItems:"50" nullable:"false"`
	Corrections    []TranscriptCorrectionView `json:"corrections" maxItems:"256" nullable:"false"`
	NextAfter      string                     `json:"nextAfter,omitempty" maxLength:"10"`
	ActivityCursor domain.Cursor              `json:"activityCursor" format:"uint64-decimal"`
}

type TranscriptReadRepository interface {
	ReadTranscript(context.Context, TranscriptReadQuery) (TranscriptReadPage, error)
}

type TranscriptReads struct {
	repository TranscriptReadRepository
}

func NewTranscriptReads(repository TranscriptReadRepository) (*TranscriptReads, error) {
	if repository == nil {
		return nil, fmt.Errorf("transcript read repository is required")
	}
	return &TranscriptReads{repository: repository}, nil
}

func (reads *TranscriptReads) Read(
	ctx context.Context,
	query TranscriptReadQuery,
) (TranscriptReadPage, error) {
	if query.ProjectID.IsZero() || query.AssetID.IsZero() ||
		(query.ArtifactID != nil && query.ArtifactID.IsZero()) || len(query.After) > 10 {
		return TranscriptReadPage{}, ErrTranscriptReadInvalid
	}
	if query.Limit == 0 {
		query.Limit = DefaultTranscriptSegmentLimit
	}
	if query.Limit > MaximumTranscriptSegmentLimit {
		return TranscriptReadPage{}, ErrTranscriptReadInvalid
	}
	return reads.repository.ReadTranscript(ctx, query)
}
