//go:build open_cut_renderer_native && cgo

package rendernative

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestPinnedNativeCaptionClosureRendersMixedTextDeterministically(t *testing.T) {
	root := os.Getenv("OPEN_CUT_NATIVE_TEXT_FONT_ROOT")
	if root == "" {
		t.Skip("OPEN_CUT_NATIVE_TEXT_FONT_ROOT is not set")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(root, renderengine.CaptionFontBundleFilename))
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := renderengine.DecodeCaptionFontBundle(manifest)
	if err != nil {
		t.Fatal(err)
	}
	native, err := New(root, bundle)
	if err != nil {
		t.Fatal(err)
	}
	language, _ := domain.ParseCaptionLanguage("he")
	captionID, _ := domain.ParseCaptionID("00000000-0000-7000-8000-000000000001")
	instruction := domain.RenderCaptionInstruction{
		CaptionID: captionID, Language: language, Text: "Open שָלום 字幕",
		Style: domain.RenderCaptionStyle{
			FontResourceID: "open-cut-caption-font-v1", FontSizeBasisPoint: 800,
			TextColorRGBA: "#ffffffff", OutlineColorRGBA: "#000000ff",
			OutlineBasisPoints: 40, LineHeightBasisPoints: 12_000,
			Alignment: "bottom-center", PositionYBasisPoint: 8_500,
			SafeWidthBasisPoint: 9_000, WrapPolicy: "explicit-lines-clip-v1",
		},
	}
	textPlan, err := renderengine.CompileCaptionTextPlan(0, instruction, 320, 180, bundle)
	if err != nil {
		t.Fatal(err)
	}
	faceByID := make(map[string]renderengine.CaptionFontFace, len(bundle.Faces))
	for _, face := range bundle.Faces {
		faceByID[face.ID] = face
	}
	probeShapes, err := native.Shape([]renderengine.CaptionShapeRequest{
		{Face: faceByID["noto-sans"], Language: language, Direction: renderengine.CaptionDirectionLTR, Text: "Open ", Font26Dot6: textPlan.Metrics.FontSize26Dot6},
		{Face: faceByID["noto-sans-hebrew"], Language: language, Direction: renderengine.CaptionDirectionRTL, Text: "שָלום", Font26Dot6: textPlan.Metrics.FontSize26Dot6},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(probeShapes) != 2 || len(probeShapes[0]) != 5 || len(probeShapes[1]) != 5 {
		t.Fatalf("probe shapes=%+v", probeShapes)
	}
	for _, glyph := range probeShapes[1] {
		if glyph.XAdvance26Dot6 < 0 {
			t.Fatalf("RTL glyph advance=%+v", glyph)
		}
	}
	first, err := renderengine.RasterizeCaptionText(textPlan, instruction, 320, 180, native)
	if err != nil {
		t.Fatal(err)
	}
	second, err := renderengine.RasterizeCaptionText(textPlan, instruction, 320, 180, native)
	if err != nil {
		t.Fatal(err)
	}
	if first.Width == 0 || first.Height == 0 || len(first.Fill) == 0 || len(first.Outline) == 0 ||
		!reflect.DeepEqual(first, second) || bytes.Count(first.Fill, []byte{0}) == len(first.Fill) {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}
