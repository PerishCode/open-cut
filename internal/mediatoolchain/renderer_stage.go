package mediatoolchain

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/PerishCode/open-cut/utils/target"
)

func buildAndStageRenderer(
	ctx context.Context,
	repositoryRoot, buildRoot, dependencyRoot, harfBuzzRoot, stageRoot string,
	buildTarget target.Target,
	archives map[string]string,
	ffmpegPath string,
	font ResourceRecord,
	stdout, stderr io.Writer,
) (ToolRecord, NoticeRecord, RendererBuildRecord, error) {
	build, err := buildRendererHelper(
		ctx, repositoryRoot, buildRoot, dependencyRoot, harfBuzzRoot,
		buildTarget, stdout, stderr,
	)
	if err != nil {
		return ToolRecord{}, NoticeRecord{}, RendererBuildRecord{}, err
	}
	relative := filepath.ToSlash(filepath.Join("media", buildTarget.ExecutableName("open-cut-render")))
	stagedPath := filepath.Join(stageRoot, filepath.FromSlash(relative))
	if err := copyRegularFile(build.Path, stagedPath, 0o755); err != nil {
		return ToolRecord{}, NoticeRecord{}, RendererBuildRecord{}, err
	}
	digest, size, err := digestFile(stagedPath)
	if err != nil || digest != build.SHA256 || size != build.ByteSize {
		return ToolRecord{}, NoticeRecord{}, RendererBuildRecord{}, fmt.Errorf("staged renderer helper changed")
	}
	ffmpegDigest, _, err := digestFile(ffmpegPath)
	if err != nil {
		return ToolRecord{}, NoticeRecord{}, RendererBuildRecord{}, err
	}
	nativeArchives := make(map[string]string, len(nativeTextSourceRecords()))
	for _, source := range nativeTextSourceRecords() {
		archive, exists := archives[source.ID]
		if !exists {
			return ToolRecord{}, NoticeRecord{}, RendererBuildRecord{}, fmt.Errorf(
				"renderer source archive %s is unavailable", source.ID,
			)
		}
		nativeArchives[source.ID] = archive
	}
	kit, err := buildRendererRelinkKit(
		ctx, repositoryRoot, buildRoot, dependencyRoot, harfBuzzRoot, buildTarget, build,
		nativeArchives,
		RendererSmokeInput{
			FFmpegPath: ffmpegPath, FFmpegSHA256: ffmpegDigest,
			FontRoot: filepath.Join(stageRoot, filepath.FromSlash(font.Root)), Font: font,
		},
		stdout, stderr,
	)
	if err != nil {
		return ToolRecord{}, NoticeRecord{}, RendererBuildRecord{}, err
	}
	record := newRendererBuildRecord(build, kit)
	if err := validateRendererBuildRecord(&record); err != nil {
		return ToolRecord{}, NoticeRecord{}, RendererBuildRecord{}, err
	}
	notice, err := stageRendererRelinkArchive(stageRoot, kit, record)
	if err != nil {
		return ToolRecord{}, NoticeRecord{}, RendererBuildRecord{}, err
	}
	if notice.ID != record.RelinkNoticeID {
		return ToolRecord{}, NoticeRecord{}, RendererBuildRecord{}, fmt.Errorf("renderer relink notice changed")
	}
	return ToolRecord{ID: "open-cut-render", Path: relative, SHA256: digest, ByteSize: size}, notice, record, nil
}
