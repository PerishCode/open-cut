package renderengine

import (
	"fmt"
	"math"
	"slices"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	CaptionBidiPolicy              = "first-strong-ltr-fallback-v1"
	MaximumCaptionGlyphExpansion   = 8
	MaximumCaptionGlyphRasterBytes = uint64(16) << 20
)

type CaptionDirection uint8

const (
	CaptionDirectionLTR CaptionDirection = 1
	CaptionDirectionRTL CaptionDirection = 2
)

type CaptionClusterCoverage uint8

const (
	CaptionClusterAbsent CaptionClusterCoverage = iota
	CaptionClusterMonochrome
	CaptionClusterColor
)

type CaptionBidiRun struct {
	FirstCluster uint32
	AfterCluster uint32
	Level        uint8
}

type CaptionFaceMetrics struct {
	UnitsPerEM uint32
	Ascender   int32
	Descender  int32
}

type CaptionShapeRequest struct {
	Face       CaptionFontFace
	Language   domain.CaptionLanguage
	Direction  CaptionDirection
	Text       string
	Font26Dot6 int32
}

type CaptionShapeGlyph struct {
	GlyphID        uint32
	XAdvance26Dot6 int32
	XOffset26Dot6  int32
	YOffset26Dot6  int32
}

type CaptionGlyphRequest struct {
	Face            CaptionFontFace
	GlyphID         uint32
	Font26Dot6      int32
	Outline26Dot6   int32
	OriginX26Dot6   int32
	BaselineY26Dot6 int32
}

type CaptionGlyphBounds struct {
	X      int32
	Y      int32
	Width  uint32
	Height uint32
}

// CaptionNativeText is the only seam implemented by the bounded native ABI.
// Calls are stateless batches: Go supplies EGC boundaries and owns fallback,
// visual assembly, metrics, clipping, coverage accumulation, and composition.
// Glyph offsets use canvas coordinates (positive Y points down); HarfBuzz's
// positive-up offsets are converted inside the native adapter.
type CaptionNativeText interface {
	FaceMetrics(face CaptionFontFace) (CaptionFaceMetrics, error)
	BidiRuns(text string, clusters []CaptionTextCluster) ([]CaptionBidiRun, error)
	ProbeClusters(
		face CaptionFontFace,
		language domain.CaptionLanguage,
		direction CaptionDirection,
		text []string,
	) ([]CaptionClusterCoverage, error)
	Shape(requests []CaptionShapeRequest) ([][]CaptionShapeGlyph, error)
	GlyphBounds(requests []CaptionGlyphRequest) ([]CaptionGlyphBounds, error)
	RasterGlyphs(
		requests []CaptionGlyphRequest,
		targets []CaptionGlyphBounds,
		fill [][]byte,
		outline [][]byte,
	) error
}

type CaptionGlyphMissingError struct {
	CaptionID domain.CaptionID
}

func (failure CaptionGlyphMissingError) Error() string {
	return "caption glyph is unavailable: " + failure.CaptionID.String()
}

type CaptionColorEmojiError struct {
	CaptionID domain.CaptionID
}

func (failure CaptionColorEmojiError) Error() string {
	return "caption color emoji is unsupported: " + failure.CaptionID.String()
}

type captionFaceSegment struct {
	face      CaptionFontFace
	first     uint32
	after     uint32
	direction CaptionDirection
}

type captionShapedSegment struct {
	face    CaptionFontFace
	glyphs  []CaptionShapeGlyph
	advance int64
}

type captionPlacedGlyph struct {
	request CaptionGlyphRequest
	bounds  CaptionGlyphBounds
}

func RasterizeCaptionText(
	plan CaptionTextPlan,
	instruction domain.RenderCaptionInstruction,
	canvasWidth uint32,
	canvasHeight uint32,
	native CaptionNativeText,
) (CaptionCoverageLayer, error) {
	if native == nil || plan.InstructionIndex > math.MaxUint32 || canvasWidth == 0 || canvasHeight == 0 ||
		plan.UnicodeDataID != CaptionUnicodeDataID || plan.TabPolicy != CaptionTabExpansionPolicy ||
		plan.Language != instruction.Language || len(plan.FaceOrder) == 0 ||
		plan.PrimaryFace.ID != plan.FaceOrder[0].ID {
		return CaptionCoverageLayer{}, fmt.Errorf("caption raster input is invalid")
	}
	primaryMetrics, err := native.FaceMetrics(plan.PrimaryFace)
	if err != nil || primaryMetrics.UnitsPerEM == 0 || primaryMetrics.Ascender <= 0 ||
		primaryMetrics.Descender > 0 || primaryMetrics.Ascender <= primaryMetrics.Descender {
		return CaptionCoverageLayer{}, fmt.Errorf("caption primary face metrics are invalid")
	}
	descender, err := scaleCaptionFontMetric(
		primaryMetrics.Descender, primaryMetrics.UnitsPerEM, plan.Metrics.FontSize26Dot6,
	)
	if err != nil {
		return CaptionCoverageLayer{}, err
	}
	bottomBaseline := int64(plan.Metrics.PositionY26Dot6) + int64(descender)
	glyphRequests := make([]CaptionGlyphRequest, 0)
	clip := captionRasterClip(plan.Metrics, canvasWidth, canvasHeight)
	for lineIndex, line := range plan.Lines {
		baseline := bottomBaseline - int64(len(plan.Lines)-1-lineIndex)*int64(plan.Metrics.LineAdvance26Dot6)
		if baseline < math.MinInt32 || baseline > math.MaxInt32 {
			return CaptionCoverageLayer{}, ResourceLimitError{Subject: "caption-layout"}
		}
		lineGlyphs, err := shapeCaptionLine(plan, instruction, line, int32(baseline), native)
		if err != nil {
			return CaptionCoverageLayer{}, err
		}
		glyphRequests = append(glyphRequests, lineGlyphs...)
	}
	bounds, err := native.GlyphBounds(glyphRequests)
	if err != nil || len(bounds) != len(glyphRequests) {
		return CaptionCoverageLayer{}, fmt.Errorf("caption glyph bounds are invalid")
	}
	placed := make([]captionPlacedGlyph, 0, len(glyphRequests))
	union := CaptionGlyphBounds{}
	hasInk := false
	for index, glyph := range glyphRequests {
		if !validCaptionGlyphBounds(bounds[index]) {
			return CaptionCoverageLayer{}, fmt.Errorf("caption glyph bounds are invalid")
		}
		visibleBounds, visible := intersectCaptionBounds(bounds[index], clip)
		if !visible {
			continue
		}
		placed = append(placed, captionPlacedGlyph{request: glyph, bounds: visibleBounds})
		if !hasInk {
			union, hasInk = visibleBounds, true
		} else {
			union = unionCaptionBounds(union, visibleBounds)
		}
	}
	if !hasInk {
		return CaptionCoverageLayer{InstructionIndex: plan.InstructionIndex}, nil
	}
	area, overflow := multiplyUint64(uint64(union.Width), uint64(union.Height))
	planes := uint64(1)
	if plan.Metrics.Outline26Dot6 != 0 {
		planes = 2
	}
	if overflow || area == 0 || area > uint64(math.MaxInt) || area > MaximumCaptionRasterBytes/planes {
		return CaptionCoverageLayer{}, ResourceLimitError{Subject: "caption-raster-bytes"}
	}
	layer := CaptionCoverageLayer{
		InstructionIndex: plan.InstructionIndex, X: union.X, Y: union.Y,
		Width: union.Width, Height: union.Height, Fill: make([]byte, int(area)),
	}
	if planes == 2 {
		layer.Outline = make([]byte, int(area))
	}
	if err := rasterCaptionGlyphs(placed, union, &layer, native); err != nil {
		return CaptionCoverageLayer{}, err
	}
	return layer, nil
}

func shapeCaptionLine(
	plan CaptionTextPlan,
	instruction domain.RenderCaptionInstruction,
	line CaptionTextLine,
	baseline int32,
	native CaptionNativeText,
) ([]CaptionGlyphRequest, error) {
	if len(line.Clusters) == 0 {
		return nil, nil
	}
	runs, err := native.BidiRuns(line.Text, line.Clusters)
	if err != nil || validateCaptionBidiRuns(runs, len(line.Clusters)) != nil {
		return nil, fmt.Errorf("caption bidi result is invalid")
	}
	directions := make([]CaptionDirection, len(line.Clusters))
	for _, run := range runs {
		direction := CaptionDirectionLTR
		if run.Level&1 != 0 {
			direction = CaptionDirectionRTL
		}
		for index := run.FirstCluster; index < run.AfterCluster; index++ {
			directions[index] = direction
		}
	}
	faces, err := selectCaptionClusterFaces(plan, instruction, line, directions, native)
	if err != nil {
		return nil, err
	}
	segments := make([]captionFaceSegment, 0, len(line.Clusters))
	for _, run := range runs {
		direction := CaptionDirectionLTR
		if run.Level&1 != 0 {
			direction = CaptionDirectionRTL
		}
		firstSegment := len(segments)
		for first := run.FirstCluster; first < run.AfterCluster; {
			after := first + 1
			for after < run.AfterCluster && faces[after].ID == faces[first].ID {
				after++
			}
			segments = append(segments, captionFaceSegment{
				face: faces[first], first: first, after: after, direction: direction,
			})
			first = after
		}
		if direction == CaptionDirectionRTL {
			slices.Reverse(segments[firstSegment:])
		}
	}
	shaped := make([]captionShapedSegment, 0, len(segments))
	shapeRequests := make([]CaptionShapeRequest, 0, len(segments))
	for _, segment := range segments {
		start := line.Clusters[segment.first].ByteStart
		end := line.Clusters[segment.after-1].ByteEnd
		shapeRequests = append(shapeRequests, CaptionShapeRequest{
			Face: segment.face, Language: plan.Language, Direction: segment.direction,
			Text: line.Text[start:end], Font26Dot6: plan.Metrics.FontSize26Dot6,
		})
	}
	shapeResults, err := native.Shape(shapeRequests)
	if err != nil || len(shapeResults) != len(shapeRequests) {
		return nil, fmt.Errorf("caption shape result is invalid")
	}
	var totalAdvance int64
	for index, segment := range segments {
		glyphs := shapeResults[index]
		if validateCaptionShape(glyphs, shapeRequests[index].Text, shapeRequests[index].Direction) != nil {
			return nil, fmt.Errorf("caption shape result is invalid")
		}
		var advance int64
		for _, glyph := range glyphs {
			advance += int64(glyph.XAdvance26Dot6)
			if advance < 0 || advance > math.MaxInt32 {
				return nil, ResourceLimitError{Subject: "caption-layout"}
			}
		}
		if math.MaxInt64-totalAdvance < advance {
			return nil, ResourceLimitError{Subject: "caption-layout"}
		}
		totalAdvance += advance
		shaped = append(shaped, captionShapedSegment{face: segment.face, glyphs: glyphs, advance: advance})
	}
	startX := int64(plan.Metrics.SafeLeft26Dot6) +
		roundHalfEvenSigned(int64(plan.Metrics.SafeWidth26Dot6)-totalAdvance, 2)
	left := startX
	result := make([]CaptionGlyphRequest, 0)
	for _, segment := range shaped {
		pen := left
		for _, glyph := range segment.glyphs {
			x := pen + int64(glyph.XOffset26Dot6)
			y := int64(baseline) + int64(glyph.YOffset26Dot6)
			if x < math.MinInt32 || x > math.MaxInt32 || y < math.MinInt32 || y > math.MaxInt32 {
				return nil, ResourceLimitError{Subject: "caption-layout"}
			}
			result = append(result, CaptionGlyphRequest{
				Face: segment.face, GlyphID: glyph.GlyphID,
				Font26Dot6: plan.Metrics.FontSize26Dot6, Outline26Dot6: plan.Metrics.Outline26Dot6,
				OriginX26Dot6: int32(x), BaselineY26Dot6: int32(y),
			})
			pen += int64(glyph.XAdvance26Dot6)
		}
		left += segment.advance
	}
	return result, nil
}

func selectCaptionClusterFaces(
	plan CaptionTextPlan,
	instruction domain.RenderCaptionInstruction,
	line CaptionTextLine,
	directions []CaptionDirection,
	native CaptionNativeText,
) ([]CaptionFontFace, error) {
	faces := make([]CaptionFontFace, len(line.Clusters))
	selected := make([]bool, len(line.Clusters))
	color := make([]bool, len(line.Clusters))
	for _, face := range plan.FaceOrder {
		for _, direction := range []CaptionDirection{CaptionDirectionLTR, CaptionDirectionRTL} {
			indices := make([]int, 0)
			texts := make([]string, 0)
			for index, cluster := range line.Clusters {
				if !selected[index] && directions[index] == direction {
					indices = append(indices, index)
					texts = append(texts, cluster.Text)
				}
			}
			if len(indices) == 0 {
				continue
			}
			coverage, err := native.ProbeClusters(face, plan.Language, direction, texts)
			if err != nil || len(coverage) != len(indices) {
				return nil, fmt.Errorf("caption cluster probe failed")
			}
			for resultIndex, value := range coverage {
				if value > CaptionClusterColor {
					return nil, fmt.Errorf("caption cluster probe failed")
				}
				index := indices[resultIndex]
				if value == CaptionClusterColor {
					color[index] = true
				}
				if value == CaptionClusterMonochrome {
					faces[index], selected[index] = face, true
				}
			}
		}
	}
	for index := range selected {
		if selected[index] {
			continue
		}
		if color[index] {
			return nil, CaptionColorEmojiError{CaptionID: instruction.CaptionID}
		}
		return nil, CaptionGlyphMissingError{CaptionID: instruction.CaptionID}
	}
	return faces, nil
}

func validateCaptionBidiRuns(runs []CaptionBidiRun, clusterCount int) error {
	if clusterCount == 0 || len(runs) == 0 || len(runs) > clusterCount {
		return fmt.Errorf("caption bidi runs are incomplete")
	}
	seen := make([]bool, clusterCount)
	for _, run := range runs {
		if run.FirstCluster >= run.AfterCluster || int(run.AfterCluster) > clusterCount {
			return fmt.Errorf("caption bidi run is invalid")
		}
		for index := run.FirstCluster; index < run.AfterCluster; index++ {
			if seen[index] {
				return fmt.Errorf("caption bidi run overlaps")
			}
			seen[index] = true
		}
	}
	for _, covered := range seen {
		if !covered {
			return fmt.Errorf("caption bidi run has a gap")
		}
	}
	return nil
}

func validateCaptionShape(
	glyphs []CaptionShapeGlyph,
	text string,
	direction CaptionDirection,
) error {
	maximum := len([]rune(text))*MaximumCaptionGlyphExpansion + 16
	if text == "" || len(glyphs) == 0 || len(glyphs) > maximum ||
		(direction != CaptionDirectionLTR && direction != CaptionDirectionRTL) {
		return fmt.Errorf("caption glyph expansion is invalid")
	}
	for _, glyph := range glyphs {
		if glyph.GlyphID == 0 || glyph.XAdvance26Dot6 < 0 {
			return fmt.Errorf("caption glyph is invalid")
		}
	}
	return nil
}

func scaleCaptionFontMetric(metric int32, unitsPerEM uint32, font26Dot6 int32) (int32, error) {
	if unitsPerEM == 0 || font26Dot6 <= 0 {
		return 0, fmt.Errorf("caption font metric scale is invalid")
	}
	value := roundHalfEvenSigned(int64(metric)*int64(font26Dot6), int64(unitsPerEM))
	if value < math.MinInt32 || value > math.MaxInt32 {
		return 0, ResourceLimitError{Subject: "caption-layout"}
	}
	return int32(value), nil
}

func captionRasterClip(metrics CaptionMetrics, width, height uint32) CaptionGlyphBounds {
	first := ceilDivSigned(int64(metrics.SafeLeft26Dot6)-32, 64)
	after := ceilDivSigned(int64(metrics.SafeLeft26Dot6)+int64(metrics.SafeWidth26Dot6)-32, 64)
	if first < 0 {
		first = 0
	}
	if after > int64(width) {
		after = int64(width)
	}
	if after < first {
		after = first
	}
	return CaptionGlyphBounds{X: int32(first), Width: uint32(after - first), Height: height}
}

func ceilDivSigned(value, divisor int64) int64 {
	quotient, remainder := value/divisor, value%divisor
	if remainder > 0 {
		quotient++
	}
	return quotient
}

func validCaptionGlyphBounds(bounds CaptionGlyphBounds) bool {
	if bounds.Width == 0 || bounds.Height == 0 {
		return bounds.Width == 0 && bounds.Height == 0
	}
	afterX := int64(bounds.X) + int64(bounds.Width)
	afterY := int64(bounds.Y) + int64(bounds.Height)
	return afterX >= math.MinInt32 && afterX <= math.MaxInt32 &&
		afterY >= math.MinInt32 && afterY <= math.MaxInt32
}

func intersectCaptionBounds(left, right CaptionGlyphBounds) (CaptionGlyphBounds, bool) {
	firstX := max(int64(left.X), int64(right.X))
	firstY := max(int64(left.Y), int64(right.Y))
	afterX := min(int64(left.X)+int64(left.Width), int64(right.X)+int64(right.Width))
	afterY := min(int64(left.Y)+int64(left.Height), int64(right.Y)+int64(right.Height))
	if afterX <= firstX || afterY <= firstY {
		return CaptionGlyphBounds{}, false
	}
	return CaptionGlyphBounds{
		X: int32(firstX), Y: int32(firstY), Width: uint32(afterX - firstX), Height: uint32(afterY - firstY),
	}, true
}

func unionCaptionBounds(left, right CaptionGlyphBounds) CaptionGlyphBounds {
	firstX := min(int64(left.X), int64(right.X))
	firstY := min(int64(left.Y), int64(right.Y))
	afterX := max(int64(left.X)+int64(left.Width), int64(right.X)+int64(right.Width))
	afterY := max(int64(left.Y)+int64(left.Height), int64(right.Y)+int64(right.Height))
	return CaptionGlyphBounds{
		X: int32(firstX), Y: int32(firstY), Width: uint32(afterX - firstX), Height: uint32(afterY - firstY),
	}
}

func rasterCaptionGlyphs(
	glyphs []captionPlacedGlyph,
	union CaptionGlyphBounds,
	layer *CaptionCoverageLayer,
	native CaptionNativeText,
) error {
	planes := uint64(1)
	if len(layer.Outline) != 0 {
		planes = 2
	}
	for first := 0; first < len(glyphs); {
		after := first
		var batchBytes uint64
		for after < len(glyphs) && glyphs[after].request.Face.ID == glyphs[first].request.Face.ID {
			area, overflow := multiplyUint64(uint64(glyphs[after].bounds.Width), uint64(glyphs[after].bounds.Height))
			bytes, byteOverflow := multiplyUint64(area, planes)
			if overflow || byteOverflow || area == 0 || area > uint64(math.MaxInt) ||
				bytes > MaximumCaptionGlyphRasterBytes {
				return ResourceLimitError{Subject: "caption-glyph-raster-bytes"}
			}
			if after != first && batchBytes > MaximumCaptionGlyphRasterBytes-bytes {
				break
			}
			batchBytes += bytes
			after++
		}
		requests := make([]CaptionGlyphRequest, after-first)
		targets := make([]CaptionGlyphBounds, after-first)
		fill := make([][]byte, after-first)
		outline := make([][]byte, after-first)
		for index := first; index < after; index++ {
			local := index - first
			requests[local], targets[local] = glyphs[index].request, glyphs[index].bounds
			area := int(uint64(glyphs[index].bounds.Width) * uint64(glyphs[index].bounds.Height))
			fill[local] = make([]byte, area)
			if planes == 2 {
				outline[local] = make([]byte, area)
			}
		}
		if err := native.RasterGlyphs(requests, targets, fill, outline); err != nil {
			return fmt.Errorf("caption glyph raster failed: %w", err)
		}
		for index := first; index < after; index++ {
			local := index - first
			glyph := glyphs[index]
			for y := uint32(0); y < glyph.bounds.Height; y++ {
				for x := uint32(0); x < glyph.bounds.Width; x++ {
					source := y*glyph.bounds.Width + x
					destination := uint32(int64(glyph.bounds.Y)-int64(union.Y))*union.Width +
						uint32(int64(glyph.bounds.X)-int64(union.X)) + y*union.Width + x
					layer.Fill[destination] = mergeCaptionCoverage(layer.Fill[destination], fill[local][source])
					if planes == 2 {
						layer.Outline[destination] = mergeCaptionCoverage(
							layer.Outline[destination], outline[local][source],
						)
					}
				}
			}
		}
		first = after
	}
	return nil
}

func mergeCaptionCoverage(back, front uint8) uint8 {
	return uint8(int64(front) + roundHalfEvenSigned(int64(back)*int64(255-front), 255))
}
