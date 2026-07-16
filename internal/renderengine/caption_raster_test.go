package renderengine

import (
	"errors"
	"reflect"
	"testing"
	"unicode/utf8"

	"github.com/PerishCode/open-cut/product/domain"
)

type fixtureCaptionNative struct {
	shapes      []CaptionShapeRequest
	invalidBidi bool
	colorOnly   bool
}

func (fixture *fixtureCaptionNative) FaceMetrics(CaptionFontFace) (CaptionFaceMetrics, error) {
	return CaptionFaceMetrics{UnitsPerEM: 1_000, Ascender: 800, Descender: -200}, nil
}

func (fixture *fixtureCaptionNative) BidiRuns(
	_ string,
	clusters []CaptionTextCluster,
) ([]CaptionBidiRun, error) {
	if fixture.invalidBidi {
		return []CaptionBidiRun{{FirstCluster: 0, AfterCluster: 1}}, nil
	}
	return []CaptionBidiRun{{FirstCluster: 0, AfterCluster: uint32(len(clusters)), Level: 1}}, nil
}

func (fixture *fixtureCaptionNative) ProbeClusters(
	face CaptionFontFace,
	_ domain.CaptionLanguage,
	_ CaptionDirection,
	texts []string,
) ([]CaptionClusterCoverage, error) {
	result := make([]CaptionClusterCoverage, len(texts))
	for index, text := range texts {
		if fixture.colorOnly {
			result[index] = CaptionClusterColor
			continue
		}
		current, _ := utf8.DecodeRuneInString(text)
		if current == 'ב' {
			if face.ID == "noto-sans-hebrew" {
				result[index] = CaptionClusterMonochrome
			}
			continue
		}
		if face.ID == "noto-sans" {
			result[index] = CaptionClusterMonochrome
		}
	}
	return result, nil
}

func (fixture *fixtureCaptionNative) Shape(requests []CaptionShapeRequest) ([][]CaptionShapeGlyph, error) {
	fixture.shapes = append(fixture.shapes, requests...)
	result := make([][]CaptionShapeGlyph, len(requests))
	for index, request := range requests {
		glyphs := make([]CaptionShapeGlyph, 0, len([]rune(request.Text)))
		for _, current := range request.Text {
			glyphs = append(glyphs, CaptionShapeGlyph{GlyphID: uint32(current), XAdvance26Dot6: 64})
		}
		result[index] = glyphs
	}
	return result, nil
}

func (fixture *fixtureCaptionNative) GlyphBounds(requests []CaptionGlyphRequest) ([]CaptionGlyphBounds, error) {
	result := make([]CaptionGlyphBounds, len(requests))
	for index, request := range requests {
		result[index] = CaptionGlyphBounds{
			X: request.OriginX26Dot6 / 64, Y: request.BaselineY26Dot6/64 - 1, Width: 1, Height: 1,
		}
	}
	return result, nil
}

func (fixture *fixtureCaptionNative) RasterGlyphs(
	_ []CaptionGlyphRequest,
	targets []CaptionGlyphBounds,
	fill [][]byte,
	outline [][]byte,
) error {
	if len(targets) != len(fill) || len(fill) != len(outline) {
		return errors.New("invalid fixture batch")
	}
	for batchIndex, target := range targets {
		if len(fill[batchIndex]) != int(target.Width*target.Height) {
			return errors.New("invalid fixture fill")
		}
		for index := range fill[batchIndex] {
			fill[batchIndex][index] = 128
		}
		for index := range outline[batchIndex] {
			outline[batchIndex][index] = 64
		}
	}
	return nil
}

func TestCaptionRasterOwnsEGCFallbackRTLAssemblyAndCoverage(t *testing.T) {
	published := captionRenderPlan(t)
	instruction := published.Plan.Payload.Captions[0]
	instruction.Text = "aבc"
	language, _ := domain.ParseCaptionLanguage("he")
	instruction.Language = language
	bundle, err := NewPinnedCaptionFontBundle(captionTextFixtureFontFiles())
	if err != nil {
		t.Fatal(err)
	}
	textPlan, err := CompileCaptionTextPlan(
		0, instruction, published.Plan.Payload.Output.CanvasWidth,
		published.Plan.Payload.Output.CanvasHeight, bundle,
	)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &fixtureCaptionNative{}
	layer, err := RasterizeCaptionText(
		textPlan, instruction, published.Plan.Payload.Output.CanvasWidth,
		published.Plan.Payload.Output.CanvasHeight, fixture,
	)
	if err != nil {
		t.Fatal(err)
	}
	shapeText := make([]string, len(fixture.shapes))
	for index, request := range fixture.shapes {
		shapeText[index] = request.Text
		if request.Direction != CaptionDirectionRTL {
			t.Fatalf("shape=%+v", request)
		}
	}
	if !reflect.DeepEqual(shapeText, []string{"c", "ב", "a"}) ||
		fixture.shapes[1].Face.ID != "noto-sans-hebrew" {
		t.Fatalf("shapes=%+v", fixture.shapes)
	}
	if layer.Width != 3 || layer.Height != 1 ||
		!reflect.DeepEqual(layer.Fill, []byte{128, 128, 128}) ||
		!reflect.DeepEqual(layer.Outline, []byte{64, 64, 64}) {
		t.Fatalf("layer=%+v", layer)
	}
}

func TestCaptionRasterPreservesEmptyLinesAsTransparentActiveLayer(t *testing.T) {
	published := captionRenderPlan(t)
	instruction := published.Plan.Payload.Captions[0]
	instruction.Text = "\n"
	bundle, _ := NewPinnedCaptionFontBundle(captionTextFixtureFontFiles())
	textPlan, err := CompileCaptionTextPlan(
		0, instruction, published.Plan.Payload.Output.CanvasWidth,
		published.Plan.Payload.Output.CanvasHeight, bundle,
	)
	if err != nil {
		t.Fatal(err)
	}
	layer, err := RasterizeCaptionText(
		textPlan, instruction, published.Plan.Payload.Output.CanvasWidth,
		published.Plan.Payload.Output.CanvasHeight, &fixtureCaptionNative{},
	)
	if err != nil || layer.Width != 0 || layer.Height != 0 || len(layer.Fill) != 0 {
		t.Fatalf("layer=%+v err=%v", layer, err)
	}
	plan := published.Plan.Payload
	plan.Captions[0] = instruction
	compositor, err := newVideoCompositor(plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compositor.CompositeFrameWithCaptions(0, nil, []CaptionCoverageLayer{layer}); err != nil {
		t.Fatal(err)
	}
}

func TestCaptionRasterRejectsBidiGapsAndColorOnlyClusters(t *testing.T) {
	published := captionRenderPlan(t)
	instruction := published.Plan.Payload.Captions[0]
	instruction.Text = "ab"
	bundle, _ := NewPinnedCaptionFontBundle(captionTextFixtureFontFiles())
	textPlan, _ := CompileCaptionTextPlan(
		0, instruction, published.Plan.Payload.Output.CanvasWidth,
		published.Plan.Payload.Output.CanvasHeight, bundle,
	)
	_, err := RasterizeCaptionText(
		textPlan, instruction, published.Plan.Payload.Output.CanvasWidth,
		published.Plan.Payload.Output.CanvasHeight, &fixtureCaptionNative{invalidBidi: true},
	)
	if err == nil {
		t.Fatal("caption raster accepted an incomplete bidi partition")
	}
	_, err = RasterizeCaptionText(
		textPlan, instruction, published.Plan.Payload.Output.CanvasWidth,
		published.Plan.Payload.Output.CanvasHeight, &fixtureCaptionNative{colorOnly: true},
	)
	var color CaptionColorEmojiError
	if !errors.As(err, &color) || color.CaptionID != instruction.CaptionID {
		t.Fatalf("err=%v", err)
	}
}

func TestCaptionCoverageMergeUsesSourceOverHalfEven(t *testing.T) {
	if actual := mergeCaptionCoverage(128, 128); actual != 192 {
		t.Fatalf("actual=%d", actual)
	}
}
