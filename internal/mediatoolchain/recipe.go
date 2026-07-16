package mediatoolchain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/PerishCode/open-cut/utils/target"
)

func digestRecipe(
	buildTarget target.Target,
	compiler string,
	ffmpegConfiguration, libvpxConfiguration, opusConfiguration []string,
	whisperConfiguration []string,
	nativeText NativeTextBuildRecipe,
	renderer RendererBuildRecord,
) (string, error) {
	encoded, err := json.Marshal(struct {
		Schema                int                           `json:"schema"`
		Target                target.Target                 `json:"target"`
		Sources               []SourceRecord                `json:"sources"`
		Compiler              string                        `json:"compiler"`
		FFmpegConfiguration   []string                      `json:"ffmpegConfiguration"`
		LibVPXConfiguration   []string                      `json:"libvpxConfiguration"`
		OpusConfiguration     []string                      `json:"opusConfiguration"`
		WhisperConfiguration  []string                      `json:"whisperConfiguration"`
		CaptionFontSelections []captionFontArchiveSelection `json:"captionFontSelections"`
		NativeText            NativeTextBuildRecipe         `json:"nativeText"`
		Renderer              RendererBuildRecord           `json:"renderer"`
	}{
		5, buildTarget, mediaSourceRecords(), compiler, ffmpegConfiguration,
		libvpxConfiguration, opusConfiguration, whisperConfiguration,
		captionFontSelections(), nativeText, renderer,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
