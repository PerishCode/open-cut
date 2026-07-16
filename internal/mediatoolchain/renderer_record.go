package mediatoolchain

import (
	"fmt"
	"reflect"
	"strings"
)

const RendererRelinkNoticeID = "open-cut-render-relink"

type RendererBuildRecord struct {
	Schema                      int                 `json:"schema"`
	ToolID                      string              `json:"toolId"`
	BuildTag                    string              `json:"buildTag"`
	GoVersion                   string              `json:"goVersion"`
	Arguments                   []string            `json:"arguments"`
	CFlags                      []string            `json:"cFlags"`
	LDFlags                     []string            `json:"ldFlags"`
	SourceSHA256                string              `json:"sourceSha256"`
	SourceFileCount             uint32              `json:"sourceFileCount"`
	LinkInputs                  []RendererLinkInput `json:"linkInputs"`
	RelinkNoticeID              string              `json:"relinkNoticeId"`
	BaselineRelinkSHA256        string              `json:"baselineRelinkSha256"`
	BaselineRelinkByteSize      uint64              `json:"baselineRelinkByteSize"`
	ModifiedRelinkSHA256        string              `json:"modifiedRelinkSha256"`
	ModifiedRelinkByteSize      uint64              `json:"modifiedRelinkByteSize"`
	ModifiedFriBidiSourceSHA256 string              `json:"modifiedFriBidiSourceSha256"`
	SmokeOutputSHA256           string              `json:"smokeOutputSha256"`
	SmokeOutputByteSize         uint64              `json:"smokeOutputByteSize"`
}

func newRendererBuildRecord(build RendererHelperBuild, kit RendererRelinkKit) RendererBuildRecord {
	return RendererBuildRecord{
		Schema: 1, ToolID: "open-cut-render", BuildTag: RendererBuildTag,
		GoVersion: build.GoVersion, Arguments: append([]string(nil), build.Arguments...),
		CFlags: append([]string(nil), build.CFlags...), LDFlags: append([]string(nil), build.LDFlags...),
		SourceSHA256: kit.SourceSHA256, SourceFileCount: uint32(len(kit.Files)),
		LinkInputs:                  append([]RendererLinkInput(nil), build.LinkInputs...),
		RelinkNoticeID:              RendererRelinkNoticeID,
		BaselineRelinkSHA256:        kit.BaselineRelinkSHA256,
		BaselineRelinkByteSize:      kit.BaselineRelinkByteSize,
		ModifiedRelinkSHA256:        kit.ModifiedRelinkSHA256,
		ModifiedRelinkByteSize:      kit.ModifiedRelinkByteSize,
		ModifiedFriBidiSourceSHA256: kit.ModifiedFriBidiSourceSHA256,
		SmokeOutputSHA256:           kit.Smoke.OutputSHA256, SmokeOutputByteSize: kit.Smoke.OutputBytes,
	}
}

func validateRendererBuildRecord(record *RendererBuildRecord) error {
	expectedArguments := []string{
		"build", "-buildvcs=false", "-trimpath", "-mod=readonly", "-tags", RendererBuildTag,
		"-ldflags=-buildid=", "-o", "$output", RendererBuildPackage,
	}
	expectedCFlags := []string{
		"-I$native/include/freetype2", "-I$native/include/fribidi", "-I$harfbuzz/src",
		"-ffile-prefix-map=$source=.",
	}
	if record == nil || record.Schema != 1 || record.ToolID != "open-cut-render" ||
		record.BuildTag != RendererBuildTag || strings.TrimSpace(record.GoVersion) != record.GoVersion ||
		record.GoVersion == "" || len(record.GoVersion) > 1024 ||
		!reflect.DeepEqual(record.Arguments, expectedArguments) ||
		!reflect.DeepEqual(record.CFlags, expectedCFlags) ||
		!reflect.DeepEqual(record.LDFlags, []string{"-L$native/lib"}) ||
		!validDigest(record.SourceSHA256) || record.SourceFileCount == 0 || record.SourceFileCount > 65_536 ||
		record.RelinkNoticeID != RendererRelinkNoticeID ||
		!validDigest(record.BaselineRelinkSHA256) || record.BaselineRelinkByteSize == 0 ||
		!validDigest(record.ModifiedRelinkSHA256) || record.ModifiedRelinkByteSize == 0 ||
		record.ModifiedRelinkSHA256 == record.BaselineRelinkSHA256 ||
		!validDigest(record.ModifiedFriBidiSourceSHA256) ||
		!validDigest(record.SmokeOutputSHA256) || record.SmokeOutputByteSize == 0 ||
		len(record.LinkInputs) != 3 {
		return fmt.Errorf("renderer build record is invalid")
	}
	expectedInputs := []struct{ id, path string }{
		{"freetype", "native/lib/libfreetype.a"},
		{"fribidi", "native/lib/libfribidi.a"},
		{"harfbuzz", "native/lib/libharfbuzz.a"},
	}
	for index, expected := range expectedInputs {
		current := record.LinkInputs[index]
		if current.ID != expected.id || current.Path != expected.path ||
			!validDigest(current.SHA256) || current.ByteSize == 0 {
			return fmt.Errorf("renderer link input record is invalid")
		}
	}
	return nil
}
