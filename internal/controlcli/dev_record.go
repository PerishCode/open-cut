package controlcli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/PerishCode/open-cut/internal/businessacceptance"
	"github.com/PerishCode/open-cut/internal/mediatoolchain"
	"github.com/PerishCode/open-cut/sidecar/protocol"
	"github.com/PerishCode/open-cut/utils/target"
)

const maximumRecordSeconds = 300

// runDevRecord captures the live development renderer as variable-frame-rate
// footage through the CDP screencast and encodes it with the repository's
// contained media toolchain. Optional --speech narrates the recording through
// macOS `say`, giving downstream transcription exact ground-truth text.
type devRecordOptions struct {
	repository, baseDir, output, speech, voice, endpoint string
	duration                                             float64
	maxWidth                                             int
}

func newDevRecordCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{Use: "record", Short: "Record the live development renderer to WebM", Args: cobra.NoArgs}
	options := devRecordOptions{}
	command.Flags().StringVar(&options.repository, "repo", ".", "open-cut repository root")
	command.Flags().StringVar(&options.baseDir, "base-dir", "", "development base directory; defaults below the repository")
	command.Flags().StringVar(&options.output, "output", "", "write the WebM recording to this path")
	command.Flags().Float64Var(&options.duration, "duration", 30, "recording length in seconds")
	command.Flags().StringVar(&options.speech, "speech", "", "narrate the recording with this exact text through macOS say")
	// The system default voice follows the OS locale and garbles or drops
	// English narration on non-English systems, so an English voice is pinned.
	command.Flags().StringVar(&options.voice, "voice", "Samantha", "say voice for the narration")
	command.Flags().IntVar(&options.maxWidth, "max-width", 1920, "cap the captured frame width")
	command.Flags().StringVar(&options.endpoint, "endpoint", "", "explicit loopback CDP origin of a controlled renderer")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		return asExit(runDevRecord(cmd.Context(), options, stdout, stderr))
	}
	return command
}

func runDevRecord(ctx context.Context, options devRecordOptions, stdout, stderr io.Writer) int {
	repository, baseDir, endpoint := &options.repository, &options.baseDir, &options.endpoint
	output, duration := &options.output, &options.duration
	speech, voice, maxWidth := &options.speech, &options.voice, &options.maxWidth
	if *output == "" || filepath.Ext(*output) != ".webm" {
		fmt.Fprintln(stderr, "dev record requires --output <path.webm>")
		return 2
	}
	if *duration <= 0 || *duration > maximumRecordSeconds {
		fmt.Fprintf(stderr, "dev record duration must be within (0, %d] seconds\n", maximumRecordSeconds)
		return 2
	}
	if *speech != "" && runtime.GOOS != "darwin" {
		fmt.Fprintln(stderr, "dev record narration requires the macOS say voice platform")
		return 2
	}
	destination, err := filepath.Abs(*output)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	repositoryRoot, err := filepath.Abs(*repository)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	verified, err := mediatoolchain.Load(
		filepath.Join(repositoryRoot, "apps", "api", "dist", "sidecar"), target.Host(),
	)
	if err != nil {
		fmt.Fprintf(stderr, "dev record requires the built media toolchain: %v\n", err)
		return 1
	}
	ffmpeg, exists := verified.Tools["ffmpeg"]
	if !exists {
		fmt.Fprintln(stderr, "the media toolchain does not contain ffmpeg")
		return 1
	}

	recordContext, cancel := context.WithTimeout(ctx, time.Duration(*duration)*time.Second+3*time.Minute)
	defer cancel()
	// An occluded window never repaints, so the screencast would capture one
	// stale frame; surface the renderer before recording starts. An explicit
	// endpoint target manages its own visibility.
	if *endpoint == "" {
		if owner, ownerErr := connectDevCell(*repository, *baseDir); ownerErr == nil {
			showContext, showCancel := context.WithTimeout(recordContext, 5*time.Second)
			_, _ = owner.Control(showContext, protocol.ControlCommandShow)
			showCancel()
		}
	}
	cdp, _, err := connectDevRenderer(recordContext, *repository, *baseDir, *endpoint, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "dev record: %v\n", err)
		return 1
	}
	defer cdp.Close()

	workRoot, err := os.MkdirTemp("", "open-cut-dev-record-")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer os.RemoveAll(workRoot)

	frames := make([]float64, 0, 256)
	err = cdp.RunScreencast(recordContext, map[string]any{
		"format": "jpeg", "quality": 85, "maxWidth": *maxWidth, "maxHeight": 1200, "everyNthFrame": 1,
	}, time.Duration(*duration*float64(time.Second)), func(frame businessacceptance.ScreencastFrame) error {
		timestamp := frame.Timestamp
		if timestamp <= 0 {
			timestamp = float64(time.Now().UnixNano()) / float64(time.Second)
		}
		filename := filepath.Join(workRoot, fmt.Sprintf("f%06d.jpg", len(frames)))
		if err := os.WriteFile(filename, frame.Data, 0o600); err != nil {
			return err
		}
		frames = append(frames, timestamp)
		return nil
	})
	if err != nil {
		fmt.Fprintf(stderr, "record development renderer: %v\n", err)
		return 1
	}
	if len(frames) == 0 {
		fmt.Fprintln(stderr, "the renderer produced no screencast frames")
		return 1
	}
	stream, videoDuration, err := writeFrameStream(workRoot, frames, *duration)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	narration := ""
	if *speech != "" {
		narration = filepath.Join(workRoot, "speech.aiff")
		sayArgs := []string{"-o", narration}
		if *voice != "" {
			sayArgs = append(sayArgs, "-v", *voice)
		}
		sayArgs = append(sayArgs, *speech)
		if err := runBoundedCommand(recordContext, "/usr/bin/say", sayArgs...); err != nil {
			fmt.Fprintf(stderr, "synthesize narration: %v\n", err)
			return 1
		}
	}

	encodeArgs := []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "image2pipe", "-c:v", "mjpeg", "-framerate", strconv.Itoa(recordGridFPS), "-i", stream,
	}
	if narration != "" {
		encodeArgs = append(encodeArgs, "-i", narration, "-map", "0:v", "-map", "1:a")
	}
	encodeArgs = append(encodeArgs,
		// JPEG screencast frames are full-range; convert to limited-range
		// Rec.709 and tag it so the footage matches what the media pipeline
		// accepts as standard SDR. Full-range/mistagged input otherwise flows
		// to the renderer's limited-range integer oracle and fails deep.
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2:in_range=full:out_range=tv,setsar=1,format=yuv420p",
		"-colorspace", "bt709", "-color_primaries", "bt709", "-color_trc", "bt709", "-color_range", "tv",
		"-c:v", "libvpx-vp9", "-crf", "34", "-b:v", "0", "-cpu-used", "5",
	)
	if narration != "" {
		// The contained filter closure has no apad; narration shorter than the
		// video simply ends early, which the product treats as trailing silence.
		encodeArgs = append(encodeArgs, "-c:a", "libopus", "-b:a", "96k")
	}
	encodeArgs = append(encodeArgs, "-t", strconv.FormatFloat(videoDuration, 'f', 3, 64), "-y", destination)
	if err := runBoundedCommand(recordContext, ffmpeg.Path, encodeArgs...); err != nil {
		fmt.Fprintf(stderr, "encode development recording: %v\n", err)
		return 1
	}
	info, err := os.Stat(destination)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return writeOutput(stdout, stderr, map[string]any{
		"schema": 1, "output": destination, "bytes": info.Size(),
		"frames": len(frames), "durationSeconds": videoDuration, "narrated": narration != "",
	})
}

const recordGridFPS = 25

// writeFrameStream renders the captured repaint timeline onto a fixed frame
// grid by repeating each JPEG for its display span (cumulative rounding keeps
// the timeline drift-free), producing one raw MJPEG stream for image2pipe.
// The contained FFmpeg has no concat demuxer, so variable durations become
// duplicated grid frames, which VP9 encodes nearly for free.
func writeFrameStream(workRoot string, timestamps []float64, requested float64) (string, float64, error) {
	clamp := func(value float64) float64 {
		return min(max(value, 0), requested)
	}
	stream := filepath.Join(workRoot, "frames.mjpg")
	output, err := os.OpenFile(stream, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, err
	}
	elapsed := 0.0
	emitted := 0
	for index := range timestamps {
		span := 0.0
		if index < len(timestamps)-1 {
			span = clamp(timestamps[index+1] - timestamps[index])
		} else {
			span = clamp(requested - (timestamps[index] - timestamps[0]))
		}
		ticks := int(float64(recordGridFPS)*(elapsed+span)+0.5) - emitted
		if index == len(timestamps)-1 && emitted+ticks == 0 {
			ticks = 1
		}
		elapsed += span
		if ticks <= 0 {
			continue
		}
		frame, readErr := os.ReadFile(filepath.Join(workRoot, fmt.Sprintf("f%06d.jpg", index)))
		if readErr != nil {
			_ = output.Close()
			return "", 0, readErr
		}
		for tick := 0; tick < ticks; tick++ {
			if _, err := output.Write(frame); err != nil {
				_ = output.Close()
				return "", 0, err
			}
			emitted++
		}
	}
	if err := output.Close(); err != nil {
		return "", 0, err
	}
	return stream, float64(emitted) / float64(recordGridFPS), nil
}

func runBoundedCommand(ctx context.Context, executable string, args ...string) error {
	command := exec.CommandContext(ctx, executable, args...)
	var diagnostics bytes.Buffer
	command.Stdout = io.Discard
	command.Stderr = &diagnostics
	if err := command.Run(); err != nil {
		detail := diagnostics.String()
		if len(detail) > 2048 {
			detail = detail[len(detail)-2048:]
		}
		return fmt.Errorf("%s: %w: %s", filepath.Base(executable), err, detail)
	}
	return nil
}
