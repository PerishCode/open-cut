package renderengine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestPinnedCaptionFontBundleIsClosedAndStrict(t *testing.T) {
	files := make([]CaptionFontFile, 0, len(pinnedCaptionFontFiles()))
	for _, filename := range pinnedCaptionFontFiles() {
		files = append(files, CaptionFontFile{
			Path: filename, SHA256: domain.Digest("sha256:" + strings.Repeat("a", 64)), ByteSize: 1,
		})
	}
	bundle, err := NewPinnedCaptionFontBundle(files)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCaptionFontBundle(encoded)
	if err != nil || len(decoded.Faces) != 20 || decoded.Faces[3].FaceIndex != 4 ||
		decoded.LanguagePolicy != CaptionCJKLanguagePolicy {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}

	bundle.Faces[3].FaceIndex = 3
	encoded, err = json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeCaptionFontBundle(encoded); err == nil {
		t.Fatal("mutated TTC face mapping was accepted")
	}
}

func TestCaptionFontFaceOrderUsesStructuredBCP47Policy(t *testing.T) {
	files := make([]CaptionFontFile, 0, len(pinnedCaptionFontFiles()))
	for _, filename := range pinnedCaptionFontFiles() {
		files = append(files, CaptionFontFile{
			Path: filename, SHA256: domain.Digest("sha256:" + strings.Repeat("b", 64)), ByteSize: 1,
		})
	}
	bundle, err := NewPinnedCaptionFontBundle(files)
	if err != nil {
		t.Fatal(err)
	}
	for value, expected := range map[string]string{
		"zh-Hant-HK": "noto-sans-cjk-hk",
		"zh-Hant":    "noto-sans-cjk-tc",
		"zh-TW":      "noto-sans-cjk-tc",
		"zh-Hans":    "noto-sans-cjk-sc",
		"zh-CN":      "noto-sans-cjk-sc",
		"zh":         "noto-sans-cjk-sc",
		"ja":         "noto-sans-cjk-jp",
		"ko":         "noto-sans-cjk-kr",
		"yue-Hant":   "noto-sans-cjk-hk",
		"und":        "noto-sans",
	} {
		captionLanguage, err := domain.ParseCaptionLanguage(value)
		if err != nil {
			t.Fatal(err)
		}
		order, err := bundle.FaceOrder(captionLanguage)
		if err != nil || len(order) != 20 {
			t.Fatalf("language=%s order=%+v err=%v", value, order, err)
		}
		if order[0].ID != expected {
			t.Fatalf("language=%s first=%+v", value, order[0])
		}
	}
}
