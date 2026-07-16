package mediatoolchain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
)

func verifyRendererConformanceMedia(
	ctx context.Context,
	ffprobe, directory, output string,
	plan application.PublishedRenderPlan,
) error {
	expected, err := application.RenderedMediaFactsForPlan(plan.Plan.Payload)
	if err != nil {
		return err
	}
	document, err := runRendererConformanceStructureProbe(ctx, ffprobe, directory, output)
	if err != nil {
		return err
	}
	if err := renderengine.ValidateRenderedMediaProbeDocument(document, expected); err != nil {
		return fmt.Errorf("renderer conformance media facts are invalid: %w; document=%+v expected=%+v", err, document, expected)
	}
	samples, err := runRendererConformanceAudioProbe(ctx, ffprobe, directory, output)
	if err != nil {
		return err
	}
	if samples != expected.AudioSampleCount.Value() {
		return fmt.Errorf("renderer conformance audio sample count is %d, expected %d", samples, expected.AudioSampleCount.Value())
	}
	return nil
}

func runRendererConformanceStructureProbe(
	ctx context.Context,
	ffprobe, directory, output string,
) (renderengine.SequencePreviewProbeDocument, error) {
	stdout := &limitedConformanceBuffer{limit: maximumConformanceOutputBytes}
	stderr := &limitedConformanceBuffer{limit: 32 << 10}
	executionContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := lifecycle.Run(executionContext, lifecycle.ProcessSpec{
		Executable: ffprobe,
		Args: []string{
			"-v", "error", "-hide_banner", "-protocol_whitelist", "file,pipe,fd",
			"-count_frames", "-show_entries",
			"format=format_name:stream=index,codec_name,codec_type,width,height,avg_frame_rate,pix_fmt," +
				"color_range,color_space,color_transfer,color_primaries,sample_rate,channels,channel_layout,nb_read_frames",
			"-of", "json=compact=1", output,
		},
		Directory: directory, Env: conformanceEnvironment(), Stdout: stdout, Stderr: stderr,
		Profile: lifecycle.ProfileHarness, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: time.Second,
	})
	if err != nil || stdout.exceeded || stderr.exceeded {
		return renderengine.SequencePreviewProbeDocument{}, fmt.Errorf(
			"renderer conformance structure probe failed (%v): %s", err, strings.TrimSpace(stderr.String()),
		)
	}
	return decodeRendererConformanceProbe(stdout.Bytes())
}

func runRendererConformanceAudioProbe(
	ctx context.Context,
	ffprobe, directory, output string,
) (uint64, error) {
	collector := renderengine.NewAudioSampleCollector(maximumConformanceOutputBytes)
	stderr := &limitedConformanceBuffer{limit: 32 << 10}
	executionContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := lifecycle.Run(executionContext, lifecycle.ProcessSpec{
		Executable: ffprobe,
		Args: []string{
			"-v", "error", "-hide_banner", "-protocol_whitelist", "file,pipe,fd",
			"-select_streams", "a:0", "-show_frames", "-show_entries", "frame=nb_samples",
			"-of", "csv=p=0", output,
		},
		Directory: directory, Env: conformanceEnvironment(), Stdin: nil, Stdout: collector, Stderr: stderr,
		Profile: lifecycle.ProfileHarness, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: time.Second,
	})
	if err != nil || stderr.exceeded {
		return 0, fmt.Errorf(
			"renderer conformance audio probe failed (%v): %s", err, strings.TrimSpace(stderr.String()),
		)
	}
	return collector.Finish()
}
