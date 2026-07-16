package renderengine

import (
	"reflect"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func captionTextFixtureFontFiles() []CaptionFontFile {
	files := make([]CaptionFontFile, 0, len(pinnedCaptionFontFiles()))
	for _, filename := range pinnedCaptionFontFiles() {
		files = append(files, CaptionFontFile{
			Path: filename, SHA256: domain.Digest("sha256:" + strings.Repeat("a", 64)), ByteSize: 1,
		})
	}
	return files
}

func TestPrepareCaptionLinesPinsExplicitLinesTabsAndUnicode15EGC(t *testing.T) {
	lines, bytes, clusters, err := prepareCaptionLines("A\t a\u0308\n\n🏳️‍🌈")
	if err != nil {
		t.Fatal(err)
	}
	if bytes != uint64(len("A     a\u0308🏳️‍🌈")) || clusters != 8 || len(lines) != 3 ||
		lines[0].Text != "A     a\u0308" || len(lines[0].Clusters) != 7 ||
		lines[0].Clusters[6].Text != "a\u0308" || len(lines[1].Clusters) != 0 ||
		len(lines[2].Clusters) != 1 || lines[2].Clusters[0].Text != "🏳️‍🌈" {
		t.Fatalf("lines=%+v bytes=%d clusters=%d", lines, bytes, clusters)
	}
	if CaptionUnicodeDataID != "unicode-egc-15.0.0-uniseg-v0.4.7" ||
		CaptionTabExpansionPolicy != "tab-exact-four-u0020-v1" {
		t.Fatal("caption Unicode closure identity drifted")
	}
}

func TestCompileCaptionTextPlanUsesLanguagePrimaryFaceAndHalfEvenMetrics(t *testing.T) {
	bundle, err := NewPinnedCaptionFontBundle(captionTextFixtureFontFiles())
	if err != nil {
		t.Fatal(err)
	}
	language, _ := domain.ParseCaptionLanguage("zh-Hant-HK")
	instruction := captionRenderPlan(t).Plan.Payload.Captions[0]
	instruction.Language = language
	instruction.Text = "字幕"
	instruction.Style.FontSizeBasisPoint = 625
	instruction.Style.OutlineBasisPoints = 25
	instruction.Style.LineHeightBasisPoints = 12_500
	instruction.Style.PositionYBasisPoint = 8_751
	instruction.Style.SafeWidthBasisPoint = 9_001
	plan, err := CompileCaptionTextPlan(7, instruction, 1_281, 721, bundle)
	if err != nil {
		t.Fatal(err)
	}
	wantMetrics := CaptionMetrics{
		FontSize26Dot6: 2884, Outline26Dot6: 115, LineAdvance26Dot6: 3605,
		PositionY26Dot6: 40381, SafeLeft26Dot6: 4095, SafeWidth26Dot6: 73794,
	}
	if plan.InstructionIndex != 7 || plan.PrimaryFace.ID != "noto-sans-cjk-hk" ||
		!reflect.DeepEqual(plan.Metrics, wantMetrics) || len(plan.Lines) != 1 {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestPrepareCaptionLinesRejectsAmbientControls(t *testing.T) {
	for _, value := range []string{"", "bad\rline", "bad\u202eline"} {
		if _, _, _, err := prepareCaptionLines(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}
