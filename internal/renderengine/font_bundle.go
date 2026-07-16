package renderengine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"reflect"
	"slices"
	"strings"

	"github.com/PerishCode/open-cut/product/domain"
	"golang.org/x/text/language"
)

const (
	CaptionFontBundleSchema   = 1
	CaptionFontBundleID       = "open-cut-caption-font-v1"
	CaptionFontBundleVersion  = "noto-sans-static-v1"
	CaptionFontBundleFilename = "font-bundle.json"
	MaximumFontBundleBytes    = 256 << 10
	CaptionCJKLanguagePolicy  = "bcp47-cjk-region-script-v1"
)

type CaptionFontBundle struct {
	Schema          int               `json:"schema"`
	ID              string            `json:"id"`
	Version         string            `json:"version"`
	DefaultFaceID   string            `json:"defaultFaceId"`
	Files           []CaptionFontFile `json:"files"`
	Faces           []CaptionFontFace `json:"faces"`
	LanguagePolicy  string            `json:"languagePolicy"`
	FallbackFaceIDs []string          `json:"fallbackFaceIds"`
}

type CaptionFontFile struct {
	Path     string        `json:"path"`
	SHA256   domain.Digest `json:"sha256"`
	ByteSize uint64        `json:"byteSize"`
}

type CaptionFontFace struct {
	ID        string   `json:"id"`
	File      string   `json:"file"`
	FaceIndex uint8    `json:"faceIndex"`
	Scripts   []string `json:"scripts"`
}

func NewPinnedCaptionFontBundle(files []CaptionFontFile) (CaptionFontBundle, error) {
	bundle := CaptionFontBundle{
		Schema: CaptionFontBundleSchema, ID: CaptionFontBundleID, Version: CaptionFontBundleVersion,
		DefaultFaceID: "noto-sans", Files: append([]CaptionFontFile(nil), files...),
		Faces:           pinnedCaptionFontFaces(),
		LanguagePolicy:  CaptionCJKLanguagePolicy,
		FallbackFaceIDs: pinnedCaptionFontFallback(),
	}
	slices.SortFunc(bundle.Files, func(left, right CaptionFontFile) int {
		return strings.Compare(left.Path, right.Path)
	})
	if err := bundle.Validate(); err != nil {
		return CaptionFontBundle{}, err
	}
	return bundle, nil
}

func DecodeCaptionFontBundle(data []byte) (CaptionFontBundle, error) {
	if len(data) == 0 || len(data) > MaximumFontBundleBytes {
		return CaptionFontBundle{}, fmt.Errorf("caption font bundle size is invalid")
	}
	var bundle CaptionFontBundle
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		bundle.Validate() != nil {
		return CaptionFontBundle{}, fmt.Errorf("caption font bundle is invalid")
	}
	return bundle, nil
}

func (bundle CaptionFontBundle) Validate() error {
	if bundle.Schema != CaptionFontBundleSchema || bundle.ID != CaptionFontBundleID ||
		bundle.Version != CaptionFontBundleVersion || bundle.DefaultFaceID != "noto-sans" ||
		bundle.LanguagePolicy != CaptionCJKLanguagePolicy ||
		len(bundle.Files) != len(pinnedCaptionFontFiles()) ||
		!reflect.DeepEqual(bundle.Faces, pinnedCaptionFontFaces()) ||
		!slices.Equal(bundle.FallbackFaceIDs, pinnedCaptionFontFallback()) {
		return fmt.Errorf("caption font bundle identity is invalid")
	}
	expectedFiles := pinnedCaptionFontFiles()
	for index, file := range bundle.Files {
		if file.Path != expectedFiles[index] || !validFontRelative(file.Path) ||
			file.ByteSize == 0 || !validDigest(file.SHA256) {
			return fmt.Errorf("caption font bundle file is invalid")
		}
	}
	return nil
}

func (bundle CaptionFontBundle) FaceOrder(captionLanguage domain.CaptionLanguage) ([]CaptionFontFace, error) {
	if err := bundle.Validate(); err != nil || captionLanguage.Validate() != nil {
		return nil, fmt.Errorf("caption font selection input is invalid")
	}
	preferred, err := preferredCJKFace(captionLanguage)
	if err != nil {
		return nil, err
	}
	faceByID := make(map[string]CaptionFontFace, len(bundle.Faces))
	for _, face := range bundle.Faces {
		faceByID[face.ID] = face
	}
	result := make([]CaptionFontFace, 0, len(bundle.FallbackFaceIDs))
	if preferred != "" {
		result = append(result, faceByID[preferred])
	}
	for _, id := range bundle.FallbackFaceIDs {
		if id != preferred {
			result = append(result, faceByID[id])
		}
	}
	return result, nil
}

func pinnedCaptionFontFiles() []string {
	return []string{
		"NotoSans-Regular.ttf",
		"NotoSansArabic-Regular.ttf",
		"NotoSansBengali-Regular.ttf",
		"NotoSansCJK-Regular.ttc",
		"NotoSansDevanagari-Regular.ttf",
		"NotoSansGujarati-Regular.ttf",
		"NotoSansGurmukhi-Regular.ttf",
		"NotoSansHebrew-Regular.ttf",
		"NotoSansKannada-Regular.ttf",
		"NotoSansKhmer-Regular.ttf",
		"NotoSansLao-Regular.ttf",
		"NotoSansMalayalam-Regular.ttf",
		"NotoSansMyanmar-Regular.ttf",
		"NotoSansTamil-Regular.ttf",
		"NotoSansTelugu-Regular.ttf",
		"NotoSansThai-Regular.ttf",
	}
}

func pinnedCaptionFontFaces() []CaptionFontFace {
	return []CaptionFontFace{
		{ID: "noto-sans", File: "NotoSans-Regular.ttf", Scripts: []string{"Cyrl", "Grek", "Latn"}},
		{ID: "noto-sans-arabic", File: "NotoSansArabic-Regular.ttf", Scripts: []string{"Arab"}},
		{ID: "noto-sans-bengali", File: "NotoSansBengali-Regular.ttf", Scripts: []string{"Beng"}},
		{ID: "noto-sans-cjk-hk", File: "NotoSansCJK-Regular.ttc", FaceIndex: 4, Scripts: []string{"Hani"}},
		{ID: "noto-sans-cjk-jp", File: "NotoSansCJK-Regular.ttc", FaceIndex: 0, Scripts: []string{"Hani", "Kana"}},
		{ID: "noto-sans-cjk-kr", File: "NotoSansCJK-Regular.ttc", FaceIndex: 1, Scripts: []string{"Hang", "Hani"}},
		{ID: "noto-sans-cjk-sc", File: "NotoSansCJK-Regular.ttc", FaceIndex: 2, Scripts: []string{"Hani"}},
		{ID: "noto-sans-cjk-tc", File: "NotoSansCJK-Regular.ttc", FaceIndex: 3, Scripts: []string{"Hani"}},
		{ID: "noto-sans-devanagari", File: "NotoSansDevanagari-Regular.ttf", Scripts: []string{"Deva"}},
		{ID: "noto-sans-gujarati", File: "NotoSansGujarati-Regular.ttf", Scripts: []string{"Gujr"}},
		{ID: "noto-sans-gurmukhi", File: "NotoSansGurmukhi-Regular.ttf", Scripts: []string{"Guru"}},
		{ID: "noto-sans-hebrew", File: "NotoSansHebrew-Regular.ttf", Scripts: []string{"Hebr"}},
		{ID: "noto-sans-kannada", File: "NotoSansKannada-Regular.ttf", Scripts: []string{"Knda"}},
		{ID: "noto-sans-khmer", File: "NotoSansKhmer-Regular.ttf", Scripts: []string{"Khmr"}},
		{ID: "noto-sans-lao", File: "NotoSansLao-Regular.ttf", Scripts: []string{"Laoo"}},
		{ID: "noto-sans-malayalam", File: "NotoSansMalayalam-Regular.ttf", Scripts: []string{"Mlym"}},
		{ID: "noto-sans-myanmar", File: "NotoSansMyanmar-Regular.ttf", Scripts: []string{"Mymr"}},
		{ID: "noto-sans-tamil", File: "NotoSansTamil-Regular.ttf", Scripts: []string{"Taml"}},
		{ID: "noto-sans-telugu", File: "NotoSansTelugu-Regular.ttf", Scripts: []string{"Telu"}},
		{ID: "noto-sans-thai", File: "NotoSansThai-Regular.ttf", Scripts: []string{"Thai"}},
	}
}

func pinnedCaptionFontFallback() []string {
	return []string{
		"noto-sans", "noto-sans-arabic", "noto-sans-hebrew", "noto-sans-devanagari",
		"noto-sans-bengali", "noto-sans-gujarati", "noto-sans-gurmukhi", "noto-sans-kannada",
		"noto-sans-malayalam", "noto-sans-tamil", "noto-sans-telugu", "noto-sans-thai",
		"noto-sans-lao", "noto-sans-khmer", "noto-sans-myanmar", "noto-sans-cjk-sc",
		"noto-sans-cjk-tc", "noto-sans-cjk-hk", "noto-sans-cjk-jp", "noto-sans-cjk-kr",
	}
}

func preferredCJKFace(captionLanguage domain.CaptionLanguage) (string, error) {
	tag, err := language.Parse(captionLanguage.String())
	if err != nil || tag.String() != captionLanguage.String() {
		return "", fmt.Errorf("caption language is invalid")
	}
	base, _ := tag.Base()
	script, _ := tag.Script()
	region, _ := tag.Region()
	switch base.String() {
	case "ja":
		return "noto-sans-cjk-jp", nil
	case "ko":
		return "noto-sans-cjk-kr", nil
	case "yue":
		return "noto-sans-cjk-hk", nil
	case "zh":
		switch region.String() {
		case "HK", "MO":
			return "noto-sans-cjk-hk", nil
		case "TW":
			return "noto-sans-cjk-tc", nil
		case "CN", "SG":
			return "noto-sans-cjk-sc", nil
		}
		if script.String() == "Hant" {
			return "noto-sans-cjk-tc", nil
		}
		return "noto-sans-cjk-sc", nil
	default:
		return "", nil
	}
}

func validFontRelative(value string) bool {
	return value != "" && value == path.Clean(value) && !path.IsAbs(value) && value != "." &&
		value != ".." && !strings.HasPrefix(value, "../") && !strings.Contains(value, "\\")
}
