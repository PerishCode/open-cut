package renderengine

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/PerishCode/open-cut/product/domain"
	"github.com/rivo/uniseg"
)

const (
	CaptionUnicodeDataID      = "unicode-egc-15.0.0-uniseg-v0.4.7"
	CaptionTabExpansionPolicy = "tab-exact-four-u0020-v1"
)

type CaptionTextCluster struct {
	Text      string
	ByteStart uint32
	ByteEnd   uint32
}

type CaptionTextLine struct {
	Text     string
	Clusters []CaptionTextCluster
}

type CaptionMetrics struct {
	FontSize26Dot6    int32
	Outline26Dot6     int32
	LineAdvance26Dot6 int32
	PositionY26Dot6   int32
	SafeLeft26Dot6    int32
	SafeWidth26Dot6   int32
}

type CaptionTextPlan struct {
	InstructionIndex uint32
	Language         domain.CaptionLanguage
	UnicodeDataID    string
	TabPolicy        string
	PrimaryFace      CaptionFontFace
	FaceOrder        []CaptionFontFace
	Metrics          CaptionMetrics
	Lines            []CaptionTextLine
}

func CompileCaptionTextPlan(
	instructionIndex uint32,
	instruction domain.RenderCaptionInstruction,
	canvasWidth uint32,
	canvasHeight uint32,
	bundle CaptionFontBundle,
) (CaptionTextPlan, error) {
	if canvasWidth == 0 || canvasHeight == 0 || instruction.Language.Validate() != nil {
		return CaptionTextPlan{}, fmt.Errorf("caption text plan input is invalid")
	}
	lines, _, _, err := prepareCaptionLines(instruction.Text)
	if err != nil {
		return CaptionTextPlan{}, err
	}
	faces, err := bundle.FaceOrder(instruction.Language)
	if err != nil || len(faces) == 0 {
		return CaptionTextPlan{}, fmt.Errorf("caption text plan face order is invalid")
	}
	metrics, err := compileCaptionMetrics(instruction.Style, canvasWidth, canvasHeight)
	if err != nil {
		return CaptionTextPlan{}, err
	}
	return CaptionTextPlan{
		InstructionIndex: instructionIndex,
		Language:         instruction.Language,
		UnicodeDataID:    CaptionUnicodeDataID,
		TabPolicy:        CaptionTabExpansionPolicy,
		PrimaryFace:      faces[0],
		FaceOrder:        faces,
		Metrics:          metrics,
		Lines:            lines,
	}, nil
}

func prepareCaptionLines(text string) ([]CaptionTextLine, uint64, uint64, error) {
	if text == "" || !utf8.ValidString(text) || len(text) > domain.MaximumAuthoredTextBytes {
		return nil, 0, 0, fmt.Errorf("caption text is invalid")
	}
	for _, current := range text {
		if !domain.IsRenderCaptionRune(current) {
			return nil, 0, 0, fmt.Errorf("caption text contains an unsupported control")
		}
	}
	logicalLines := strings.Split(text, "\n")
	lines := make([]CaptionTextLine, 0, len(logicalLines))
	var expandedBytes, clusterCount uint64
	for _, logical := range logicalLines {
		expanded := strings.ReplaceAll(logical, "\t", "    ")
		if math.MaxUint64-expandedBytes < uint64(len(expanded)) {
			return nil, 0, 0, ResourceLimitError{Subject: "caption-text-bytes"}
		}
		expandedBytes += uint64(len(expanded))
		line := CaptionTextLine{Text: expanded, Clusters: []CaptionTextCluster{}}
		remaining, state := expanded, -1
		for len(remaining) != 0 {
			cluster, rest, _, nextState := uniseg.FirstGraphemeClusterInString(remaining, state)
			if cluster == "" || len(rest) >= len(remaining) {
				return nil, 0, 0, fmt.Errorf("caption grapheme segmentation failed")
			}
			start := len(expanded) - len(remaining)
			end := start + len(cluster)
			if start > math.MaxUint32 || end > math.MaxUint32 {
				return nil, 0, 0, ResourceLimitError{Subject: "caption-text-bytes"}
			}
			line.Clusters = append(line.Clusters, CaptionTextCluster{
				Text: cluster, ByteStart: uint32(start), ByteEnd: uint32(end),
			})
			clusterCount++
			remaining, state = rest, nextState
		}
		lines = append(lines, line)
	}
	return lines, expandedBytes, clusterCount, nil
}

func compileCaptionMetrics(
	style domain.RenderCaptionStyle,
	canvasWidth uint32,
	canvasHeight uint32,
) (CaptionMetrics, error) {
	fontSize, err := captionBasisPoints26Dot6(canvasHeight, style.FontSizeBasisPoint)
	if err != nil || fontSize <= 0 {
		return CaptionMetrics{}, fmt.Errorf("caption font metrics are invalid")
	}
	outline, err := captionBasisPoints26Dot6(canvasHeight, style.OutlineBasisPoints)
	if err != nil {
		return CaptionMetrics{}, fmt.Errorf("caption outline metrics are invalid")
	}
	position, err := captionBasisPoints26Dot6(canvasHeight, style.PositionYBasisPoint)
	if err != nil {
		return CaptionMetrics{}, fmt.Errorf("caption position metrics are invalid")
	}
	safeWidth, err := captionBasisPoints26Dot6(canvasWidth, style.SafeWidthBasisPoint)
	if err != nil || safeWidth <= 0 {
		return CaptionMetrics{}, fmt.Errorf("caption safe width metrics are invalid")
	}
	lineAdvance := roundHalfEvenSigned(int64(fontSize)*int64(style.LineHeightBasisPoints), 10_000)
	canvas26Dot6 := int64(canvasWidth) * 64
	safeLeft := roundHalfEvenSigned(canvas26Dot6-int64(safeWidth), 2)
	for _, value := range []int64{int64(fontSize), int64(outline), int64(position), int64(safeWidth), lineAdvance, safeLeft} {
		if value < math.MinInt32 || value > math.MaxInt32 {
			return CaptionMetrics{}, fmt.Errorf("caption metrics overflow")
		}
	}
	return CaptionMetrics{
		FontSize26Dot6: int32(fontSize), Outline26Dot6: int32(outline),
		LineAdvance26Dot6: int32(lineAdvance), PositionY26Dot6: int32(position),
		SafeLeft26Dot6: int32(safeLeft), SafeWidth26Dot6: int32(safeWidth),
	}, nil
}

func captionBasisPoints26Dot6(basis uint32, points uint16) (int64, error) {
	if basis == 0 || points > 10_000 {
		return 0, fmt.Errorf("caption basis-point value is invalid")
	}
	return roundHalfEvenSigned(int64(basis)*int64(points)*64, 10_000), nil
}
