package mediatoolchain

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/utils/atomicfile"
)

const captionFontResourceRoot = renderengine.CaptionFontResourceRoot

type captionFontArchiveSelection struct {
	SourceID    string `json:"sourceId"`
	Member      string `json:"member"`
	Destination string `json:"destination"`
}

func captionFontSourceRecords() []SourceRecord {
	return []SourceRecord{
		{ID: "noto-sans", Version: "2.015", URL: "https://github.com/notofonts/latin-greek-cyrillic/releases/download/NotoSans-v2.015/NotoSans-v2.015.zip", SHA256: "sha256:0c34df072a3fa7efbb7cbf34950e1f971a4447cffe365d3a359e2d4089b958f5", License: "OFL-1.1"},
		{ID: "noto-sans-arabic", Version: "2.013", URL: "https://github.com/notofonts/arabic/releases/download/NotoSansArabic-v2.013/NotoSansArabic-v2.013.zip", SHA256: "sha256:1301aceaea84c501cf2e6dcfb3182e2328c8eae5725817fcb239672bda7154f1", License: "OFL-1.1"},
		{ID: "noto-sans-bengali", Version: "3.011", URL: "https://github.com/notofonts/bengali/releases/download/NotoSansBengali-v3.011/NotoSansBengali-v3.011.zip", SHA256: "sha256:2aca24bf71665e66c32e14862ee58857dd8b8b07b6321ec7c96f3121ad6683b1", License: "OFL-1.1"},
		{ID: "noto-sans-cjk", Version: "2.004", URL: "https://github.com/notofonts/noto-cjk/releases/download/Sans2.004/03_NotoSansCJK-OTC.zip", SHA256: "sha256:528f4e1b25ff3badb0321b38d015d954c4c0de926c7830ef50e4a1948f6a3eed", License: "OFL-1.1"},
		{ID: "noto-sans-devanagari", Version: "2.006", URL: "https://github.com/notofonts/devanagari/releases/download/NotoSansDevanagari-v2.006/NotoSansDevanagari-v2.006.zip", SHA256: "sha256:4c582c103f0a42836338df07148b23a0aa080cce8393ddc4364af87eb22ebd85", License: "OFL-1.1"},
		{ID: "noto-sans-gujarati", Version: "2.106", URL: "https://github.com/notofonts/gujarati/releases/download/NotoSansGujarati-v2.106/NotoSansGujarati-v2.106.zip", SHA256: "sha256:a4ff1dd03a4998ba08ee7ee43be2aca7594a884982049e48c25ccc48991474ed", License: "OFL-1.1"},
		{ID: "noto-sans-gurmukhi", Version: "2.004", URL: "https://github.com/notofonts/gurmukhi/releases/download/NotoSansGurmukhi-v2.004/NotoSansGurmukhi-v2.004.zip", SHA256: "sha256:a55ee5ee831ce3a4bb73bdc46bd0a02db6eb9445c370a0afbc529379ed8cea52", License: "OFL-1.1"},
		{ID: "noto-sans-hebrew", Version: "3.001", URL: "https://github.com/notofonts/hebrew/releases/download/NotoSansHebrew-v3.001/NotoSansHebrew-v3.001.zip", SHA256: "sha256:df0a71814b4e63644cf40fcc4529111b61266b7a2dafbe95068b29a7520cc3cb", License: "OFL-1.1"},
		{ID: "noto-sans-kannada", Version: "2.006", URL: "https://github.com/notofonts/kannada/releases/download/NotoSansKannada-v2.006/NotoSansKannada-v2.006.zip", SHA256: "sha256:902b1b92018f7c96862d68d0a39ef03b978203cd4c3f55d6760f597657b51c44", License: "OFL-1.1"},
		{ID: "noto-sans-khmer", Version: "2.004", URL: "https://github.com/notofonts/khmer/releases/download/NotoSansKhmer-v2.004/NotoSansKhmer-v2.004.zip", SHA256: "sha256:19382ca97d62febea1c735ebee35a5aa4f03beca9b6ea6f6d86b7a7a0025a688", License: "OFL-1.1"},
		{ID: "noto-sans-lao", Version: "2.003", URL: "https://github.com/notofonts/lao/releases/download/NotoSansLao-v2.003/NotoSansLao-v2.003.zip", SHA256: "sha256:5a87c31b1a40ef8147c1e84437e5f0ceba2d4dbbfc0b56a65821ad29870da8c0", License: "OFL-1.1"},
		{ID: "noto-sans-malayalam", Version: "2.104", URL: "https://github.com/notofonts/malayalam/releases/download/NotoSansMalayalam-v2.104/NotoSansMalayalam-v2.104.zip", SHA256: "sha256:2ebd31e79f2893025d659def7784e0ec3557e7ff9ac105adcc82d35782913bf2", License: "OFL-1.1"},
		{ID: "noto-sans-myanmar", Version: "2.107", URL: "https://github.com/notofonts/myanmar/releases/download/NotoSansMyanmar-v2.107/NotoSansMyanmar-v2.107.zip", SHA256: "sha256:c4995ee97f1f267b46cf83734dbf18a3cfd431e387b6fe38e90279546f260c4b", License: "OFL-1.1"},
		{ID: "noto-sans-tamil", Version: "2.004", URL: "https://github.com/notofonts/tamil/releases/download/NotoSansTamil-v2.004/NotoSansTamil-v2.004.zip", SHA256: "sha256:f8284e0f200a7f29a439b4ec88280d864b2b31f8479111c5b658ba6da38b3005", License: "OFL-1.1"},
		{ID: "noto-sans-telugu", Version: "2.005", URL: "https://github.com/notofonts/telugu/releases/download/NotoSansTelugu-v2.005/NotoSansTelugu-v2.005.zip", SHA256: "sha256:3553e00ca341dc06f4a143c604dd93a1342553169b5a06dc8b0ff50ab6eba0a2", License: "OFL-1.1"},
		{ID: "noto-sans-thai", Version: "2.002", URL: "https://github.com/notofonts/thai/releases/download/NotoSansThai-v2.002/NotoSansThai-v2.002.zip", SHA256: "sha256:af889cc673fc714060ce5e4e088fbad32aa4c0571a19958efeaff128a22da485", License: "OFL-1.1"},
	}
}

func captionFontSelections() []captionFontArchiveSelection {
	return []captionFontArchiveSelection{
		{SourceID: "noto-sans", Member: "NotoSans/full/ttf/NotoSans-Regular.ttf", Destination: "NotoSans-Regular.ttf"},
		{SourceID: "noto-sans-arabic", Member: "NotoSansArabic/full/ttf/NotoSansArabic-Regular.ttf", Destination: "NotoSansArabic-Regular.ttf"},
		{SourceID: "noto-sans-bengali", Member: "NotoSansBengali/full/ttf/NotoSansBengali-Regular.ttf", Destination: "NotoSansBengali-Regular.ttf"},
		{SourceID: "noto-sans-cjk", Member: "NotoSansCJK-Regular.ttc", Destination: "NotoSansCJK-Regular.ttc"},
		{SourceID: "noto-sans-devanagari", Member: "NotoSansDevanagari/full/ttf/NotoSansDevanagari-Regular.ttf", Destination: "NotoSansDevanagari-Regular.ttf"},
		{SourceID: "noto-sans-gujarati", Member: "NotoSansGujarati/full/ttf/NotoSansGujarati-Regular.ttf", Destination: "NotoSansGujarati-Regular.ttf"},
		{SourceID: "noto-sans-gurmukhi", Member: "NotoSansGurmukhi/full/ttf/NotoSansGurmukhi-Regular.ttf", Destination: "NotoSansGurmukhi-Regular.ttf"},
		{SourceID: "noto-sans-hebrew", Member: "NotoSansHebrew/full/ttf/NotoSansHebrew-Regular.ttf", Destination: "NotoSansHebrew-Regular.ttf"},
		{SourceID: "noto-sans-kannada", Member: "NotoSansKannada/full/ttf/NotoSansKannada-Regular.ttf", Destination: "NotoSansKannada-Regular.ttf"},
		{SourceID: "noto-sans-khmer", Member: "NotoSansKhmer/full/ttf/NotoSansKhmer-Regular.ttf", Destination: "NotoSansKhmer-Regular.ttf"},
		{SourceID: "noto-sans-lao", Member: "NotoSansLao/full/ttf/NotoSansLao-Regular.ttf", Destination: "NotoSansLao-Regular.ttf"},
		{SourceID: "noto-sans-malayalam", Member: "NotoSansMalayalam/full/ttf/NotoSansMalayalam-Regular.ttf", Destination: "NotoSansMalayalam-Regular.ttf"},
		{SourceID: "noto-sans-myanmar", Member: "NotoSansMyanmar/full/ttf/NotoSansMyanmar-Regular.ttf", Destination: "NotoSansMyanmar-Regular.ttf"},
		{SourceID: "noto-sans-tamil", Member: "NotoSansTamil/full/ttf/NotoSansTamil-Regular.ttf", Destination: "NotoSansTamil-Regular.ttf"},
		{SourceID: "noto-sans-telugu", Member: "NotoSansTelugu/full/ttf/NotoSansTelugu-Regular.ttf", Destination: "NotoSansTelugu-Regular.ttf"},
		{SourceID: "noto-sans-thai", Member: "NotoSansThai/full/ttf/NotoSansThai-Regular.ttf", Destination: "NotoSansThai-Regular.ttf"},
	}
}

func stageCaptionFontBundle(archives map[string]string, stageRoot string) (ResourceRecord, error) {
	resourceRoot := filepath.Join(stageRoot, filepath.FromSlash(captionFontResourceRoot))
	for _, selection := range captionFontSelections() {
		archive, exists := archives[selection.SourceID]
		if !exists {
			return ResourceRecord{}, fmt.Errorf("caption font source %s is unavailable", selection.SourceID)
		}
		if err := extractZipFiles(archive, resourceRoot, []archiveSelection{{
			Member: selection.Member, Destination: selection.Destination,
		}}); err != nil {
			return ResourceRecord{}, fmt.Errorf("stage caption font %s: %w", selection.SourceID, err)
		}
	}
	files := make([]renderengine.CaptionFontFile, 0, len(captionFontSelections()))
	for _, selection := range captionFontSelections() {
		digest, size, err := digestFile(filepath.Join(resourceRoot, selection.Destination))
		if err != nil {
			return ResourceRecord{}, err
		}
		files = append(files, renderengine.CaptionFontFile{
			Path: selection.Destination, SHA256: domain.Digest(digest), ByteSize: size,
		})
	}
	bundle, err := renderengine.NewPinnedCaptionFontBundle(files)
	if err != nil {
		return ResourceRecord{}, err
	}
	manifestPath := filepath.Join(resourceRoot, renderengine.CaptionFontBundleFilename)
	if err := atomicfile.WriteJSON(manifestPath, bundle, 0o600); err != nil {
		return ResourceRecord{}, err
	}
	manifestDigest, manifestSize, err := digestFile(manifestPath)
	if err != nil {
		return ResourceRecord{}, err
	}
	record := ResourceRecord{
		ID: renderengine.CaptionFontBundleID, Kind: ResourceKindFontBundle,
		Version: renderengine.CaptionFontBundleVersion, Root: captionFontResourceRoot,
		Files: make([]ResourceFileRecord, 0, len(files)+1),
	}
	for _, file := range files {
		record.Files = append(record.Files, ResourceFileRecord{
			Path: file.Path, SHA256: file.SHA256.String(), ByteSize: file.ByteSize,
		})
	}
	record.Files = append(record.Files, ResourceFileRecord{
		Path: renderengine.CaptionFontBundleFilename, SHA256: manifestDigest, ByteSize: manifestSize,
	})
	slices.SortFunc(record.Files, func(left, right ResourceFileRecord) int {
		return strings.Compare(left.Path, right.Path)
	})
	record.SHA256, err = resourceClosureDigest(record)
	return record, err
}

func stageCaptionFontNotices(archives map[string]string, stageRoot string) ([]NoticeRecord, error) {
	definitions := []struct {
		id, sourceID, member, relative string
	}{
		{"noto-sans-ofl", "noto-sans", "OFL.txt", "licenses/media/NOTO-SANS-OFL.txt"},
		{"noto-sans-cjk-ofl", "noto-sans-cjk", "LICENSE", "licenses/media/NOTO-SANS-CJK-OFL.txt"},
	}
	result := make([]NoticeRecord, 0, len(definitions))
	for _, definition := range definitions {
		archive, exists := archives[definition.sourceID]
		if !exists {
			return nil, fmt.Errorf("caption font notice source %s is unavailable", definition.sourceID)
		}
		if err := extractZipFiles(archive, stageRoot, []archiveSelection{{
			Member: definition.member, Destination: definition.relative,
		}}); err != nil {
			return nil, err
		}
		digest, size, err := digestFile(filepath.Join(stageRoot, filepath.FromSlash(definition.relative)))
		if err != nil {
			return nil, err
		}
		result = append(result, NoticeRecord{
			ID: definition.id, Path: definition.relative, SHA256: digest, ByteSize: size,
		})
	}
	return result, nil
}
