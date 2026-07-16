package transcriptadapter

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"golang.org/x/text/language"
)

const maximumWhisperResultBytes = 128 << 20

type whisperJSON struct {
	SystemInfo string `json:"systeminfo"`
	Model      struct {
		Type         string `json:"type"`
		Multilingual bool   `json:"multilingual"`
		Vocab        int64  `json:"vocab"`
		Audio        struct {
			Context int64 `json:"ctx"`
			State   int64 `json:"state"`
			Head    int64 `json:"head"`
			Layer   int64 `json:"layer"`
		} `json:"audio"`
		Text struct {
			Context int64 `json:"ctx"`
			State   int64 `json:"state"`
			Head    int64 `json:"head"`
			Layer   int64 `json:"layer"`
		} `json:"text"`
		Mels  int64 `json:"mels"`
		FType int64 `json:"ftype"`
	} `json:"model"`
	Params struct {
		Model     string `json:"model"`
		Language  string `json:"language"`
		Translate bool   `json:"translate"`
	} `json:"params"`
	Result struct {
		Language string `json:"language"`
	} `json:"result"`
	Transcription []whisperSegmentJSON `json:"transcription"`
}

type whisperSegmentJSON struct {
	Timestamps whisperTimestampsJSON `json:"timestamps"`
	Offsets    whisperOffsetsJSON    `json:"offsets"`
	Text       string                `json:"text"`
	Tokens     []whisperTokenJSON    `json:"tokens"`
}

type whisperTokenJSON struct {
	Text        string                 `json:"text"`
	Timestamps  *whisperTimestampsJSON `json:"timestamps,omitempty"`
	Offsets     *whisperOffsetsJSON    `json:"offsets,omitempty"`
	ID          int64                  `json:"id"`
	Probability float64                `json:"p"`
	DTW         float64                `json:"t_dtw"`
}

type whisperTimestampsJSON struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type whisperOffsetsJSON struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

func DecodeWhisper(
	path string,
	proof domain.TranscriptNormalizationProof,
) (application.TranscriptRecognition, error) {
	if proof.Validate() != nil {
		return application.TranscriptRecognition{}, domain.ErrInvalidTranscript
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() <= 0 || info.Size() > maximumWhisperResultBytes {
		return application.TranscriptRecognition{}, domain.ErrInvalidTranscript
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return application.TranscriptRecognition{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document whisperJSON
	if err := decoder.Decode(&document); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		document.SystemInfo == "" || len(document.SystemInfo) > 16<<10 || document.Model.Type == "" ||
		!document.Model.Multilingual || document.Model.Vocab <= 0 || document.Params.Model == "" ||
		document.Params.Language != "auto" || document.Params.Translate ||
		len(document.Transcription) > domain.MaximumTranscriptSegments {
		return application.TranscriptRecognition{}, domain.ErrInvalidTranscript
	}
	detectedLanguage, err := canonicalWhisperLanguage(document.Result.Language)
	if err != nil {
		return application.TranscriptRecognition{}, err
	}
	result := application.TranscriptRecognition{
		DetectedLanguage: detectedLanguage, Normalization: proof,
		Segments: make([]application.TranscriptSegmentRecognition, 0, len(document.Transcription)),
	}
	for _, input := range document.Transcription {
		segment, empty, err := normalizeWhisperSegment(input, proof)
		if err != nil {
			return application.TranscriptRecognition{}, err
		}
		if empty {
			continue
		}
		if len(result.Segments) > 0 {
			previousEnd, _ := result.Segments[len(result.Segments)-1].SourceRange.End()
			comparison, compareErr := segment.SourceRange.Start.Compare(previousEnd)
			if compareErr != nil || comparison < 0 {
				return application.TranscriptRecognition{}, domain.ErrInvalidTranscript
			}
		}
		result.Segments = append(result.Segments, segment)
	}
	return result, nil
}

func normalizeWhisperSegment(
	input whisperSegmentJSON,
	proof domain.TranscriptNormalizationProof,
) (application.TranscriptSegmentRecognition, bool, error) {
	if !validWhisperTimestamp(input.Timestamps) || len(input.Tokens) > domain.MaximumTranscriptTokensPerSegment {
		return application.TranscriptSegmentRecognition{}, false, domain.ErrInvalidTranscript
	}
	text := strings.TrimSpace(input.Text)
	if text == "" {
		return application.TranscriptSegmentRecognition{}, true, nil
	}
	if !validTranscriptAdapterText(text, domain.MaximumTranscriptSegmentBytes, true) {
		return application.TranscriptSegmentRecognition{}, false, domain.ErrInvalidTranscript
	}
	segmentRange, err := transcriptRangeFromMilliseconds(proof, input.Offsets.From, input.Offsets.To)
	if err != nil {
		return application.TranscriptSegmentRecognition{}, false, err
	}
	tokens := make([]application.TranscriptTokenRecognition, 0, len(input.Tokens))
	pendingPrefix := ""
	for _, inputToken := range input.Tokens {
		if !isFiniteProbability(inputToken.Probability) || math.IsNaN(inputToken.DTW) || math.IsInf(inputToken.DTW, 0) {
			return application.TranscriptSegmentRecognition{}, false, domain.ErrInvalidTranscript
		}
		if inputToken.Offsets == nil {
			continue
		}
		if inputToken.Timestamps == nil || !validWhisperTimestamp(*inputToken.Timestamps) {
			return application.TranscriptSegmentRecognition{}, false, domain.ErrInvalidTranscript
		}
		if inputToken.Offsets.From < input.Offsets.From || inputToken.Offsets.To > input.Offsets.To ||
			inputToken.Offsets.To < inputToken.Offsets.From {
			return application.TranscriptSegmentRecognition{}, false, domain.ErrInvalidTranscript
		}
		if inputToken.Offsets.From == inputToken.Offsets.To {
			if whisperControlToken(inputToken.Text) {
				continue
			}
			if !validTranscriptAdapterText(inputToken.Text, domain.MaximumTranscriptTokenBytes, false) {
				return application.TranscriptSegmentRecognition{}, false, domain.ErrInvalidTranscript
			}
			if len(tokens) == 0 {
				pendingPrefix += inputToken.Text
				if len([]byte(pendingPrefix)) > domain.MaximumTranscriptTokenBytes {
					return application.TranscriptSegmentRecognition{}, false, domain.ErrInvalidTranscript
				}
			} else {
				last := len(tokens) - 1
				tokens[last].Text += inputToken.Text
				tokens[last].ConfidenceBasisPoints = nil
			}
			continue
		}
		tokenRange, err := transcriptRangeFromMilliseconds(proof, inputToken.Offsets.From, inputToken.Offsets.To)
		if err != nil || !transcriptRangeContains(segmentRange, tokenRange) {
			return application.TranscriptSegmentRecognition{}, false, domain.ErrInvalidTranscript
		}
		confidence := uint16(math.Round(inputToken.Probability * 10_000))
		tokenText := pendingPrefix + inputToken.Text
		confidencePointer := &confidence
		if pendingPrefix != "" {
			confidencePointer = nil
			pendingPrefix = ""
		}
		tokens = append(tokens, application.TranscriptTokenRecognition{
			SourceRange: tokenRange, Text: tokenText, ConfidenceBasisPoints: confidencePointer,
		})
	}
	if pendingPrefix != "" {
		return application.TranscriptSegmentRecognition{}, false, domain.ErrInvalidTranscript
	}
	trimWhisperTokenEdges(&tokens)
	if len(tokens) == 0 {
		return application.TranscriptSegmentRecognition{}, false, domain.ErrInvalidTranscript
	}
	var lexical strings.Builder
	var previousEnd *domain.RationalTime
	for _, token := range tokens {
		if !validTranscriptAdapterText(token.Text, domain.MaximumTranscriptTokenBytes, false) {
			return application.TranscriptSegmentRecognition{}, false, domain.ErrInvalidTranscript
		}
		if previousEnd != nil {
			comparison, err := token.SourceRange.Start.Compare(*previousEnd)
			if err != nil || comparison < 0 {
				return application.TranscriptSegmentRecognition{}, false, domain.ErrInvalidTranscript
			}
		}
		end, _ := token.SourceRange.End()
		previousEnd = &end
		lexical.WriteString(token.Text)
	}
	if lexical.String() != text {
		return application.TranscriptSegmentRecognition{}, false, domain.ErrInvalidTranscript
	}
	return application.TranscriptSegmentRecognition{SourceRange: segmentRange, Text: text, Tokens: tokens}, false, nil
}

func transcriptRangeFromMilliseconds(
	proof domain.TranscriptNormalizationProof,
	from, to int64,
) (domain.TimeRange, error) {
	if from < 0 || to <= from || from > math.MaxInt64/16 || to > math.MaxInt64/16 {
		return domain.TimeRange{}, domain.ErrInvalidTranscript
	}
	startSample, endSample := from*16, to*16
	maximum := int64(proof.SampleCount.Value())
	if startSample >= maximum {
		return domain.TimeRange{}, domain.ErrInvalidTranscript
	}
	if endSample > maximum {
		endSample = maximum
	}
	startOffset, err := domain.NewRationalTime(startSample, domain.TranscriptSampleRate)
	if err != nil {
		return domain.TimeRange{}, err
	}
	start, err := proof.SourceStartTime.Add(startOffset)
	if err != nil {
		return domain.TimeRange{}, err
	}
	duration, err := domain.NewRationalTime(endSample-startSample, domain.TranscriptSampleRate)
	if err != nil {
		return domain.TimeRange{}, err
	}
	return domain.NewTimeRange(start, duration)
}

func transcriptRangeContains(parent, child domain.TimeRange) bool {
	parentEnd, parentErr := parent.End()
	childEnd, childErr := child.End()
	startComparison, startErr := child.Start.Compare(parent.Start)
	endComparison, endErr := childEnd.Compare(parentEnd)
	return parentErr == nil && childErr == nil && startErr == nil && endErr == nil &&
		startComparison >= 0 && endComparison <= 0
}

func trimWhisperTokenEdges(tokens *[]application.TranscriptTokenRecognition) {
	values := *tokens
	for len(values) > 0 {
		values[0].Text = strings.TrimLeftFunc(values[0].Text, unicode.IsSpace)
		if values[0].Text != "" {
			break
		}
		values = values[1:]
	}
	for len(values) > 0 {
		last := len(values) - 1
		values[last].Text = strings.TrimRightFunc(values[last].Text, unicode.IsSpace)
		if values[last].Text != "" {
			break
		}
		values = values[:last]
	}
	*tokens = values
}

func whisperControlToken(value string) bool {
	if len(value) < 4 || !strings.HasPrefix(value, "[_") || !strings.HasSuffix(value, "]") {
		return false
	}
	for _, current := range value[2 : len(value)-1] {
		if current != '_' && (current < '0' || current > '9') && (current < 'A' || current > 'Z') {
			return false
		}
	}
	return true
}

func canonicalWhisperLanguage(value string) (string, error) {
	if value == "" || len(value) > domain.MaximumCaptionLanguageBytes || strings.TrimSpace(value) != value {
		return "", domain.ErrInvalidTranscript
	}
	tag, err := language.Parse(value)
	if err != nil || len(tag.Extensions()) != 0 {
		return "", domain.ErrInvalidTranscript
	}
	canonical := tag.String()
	if _, err := domain.ParseCaptionLanguage(canonical); err != nil {
		return "", domain.ErrInvalidTranscript
	}
	return canonical, nil
}

func validWhisperTimestamp(value whisperTimestampsJSON) bool {
	return value.From != "" && value.To != "" && len(value.From) <= 32 && len(value.To) <= 32
}

func isFiniteProbability(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func validTranscriptAdapterText(value string, maximum int, trim bool) bool {
	if value == "" || len([]byte(value)) > maximum || !utf8.ValidString(value) ||
		(trim && strings.TrimSpace(value) != value) {
		return false
	}
	for _, current := range value {
		if current < 0x20 || current == 0x7f {
			return false
		}
	}
	return true
}
