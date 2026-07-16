package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/lifecycle"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

// inventoryProxyAudioSamples establishes the exact renderer-visible S16
// sample count after Opus pre-skip and end discard. It deliberately selects
// the pinned libopus decoder instead of FFmpeg's native Opus decoder.
func (executor *ExternalMediaProxyExecutor) inventoryProxyAudioSamples(
	ctx context.Context,
	directory string,
	source string,
	descriptor domain.SourceStreamDescriptor,
) (domain.UInt64, error) {
	scanContext, stop := context.WithCancel(ctx)
	defer stop()
	collector := &proxyAudioSampleCollector{
		maximum: application.MaximumSourceProxyAudioSamples,
		stop:    stop,
	}
	stderr := &boundedBuffer{limit: maximumExecutorOutputBytes}
	err := lifecycle.Run(scanContext, lifecycle.ProcessSpec{
		Executable: executor.probe,
		Args: []string{
			"-v", "error", "-hide_banner", "-cpuflags", "0", "-protocol_whitelist", "file",
			"-c:a", "libopus", "-request_sample_fmt", "s16", "-threads", "1",
			"-select_streams", strconv.FormatUint(uint64(descriptor.Index), 10),
			"-show_frames", "-show_entries", "frame=sample_fmt,nb_samples", "-of", "csv=p=0", source,
		},
		Directory: directory, Env: executorEnvironment(), Stdout: collector, Stderr: stderr,
		Profile: executor.profile, Presentation: lifecycle.PresentationHeadless,
		ContainProcessTree: true, TerminationGrace: 2 * time.Second,
	})
	if finishErr := collector.Finish(); finishErr != nil {
		return 0, finishErr
	}
	if stderr.exceeded {
		return 0, fmt.Errorf("audio sample diagnostics exceeded the limit")
	}
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, fmt.Errorf("audio sample inventory failed: %s", strings.TrimSpace(stderr.String()))
	}
	return domain.NewUInt64(collector.total)
}

type proxyAudioSampleCollector struct {
	maximum uint64
	stop    context.CancelFunc
	buffer  []byte
	total   uint64
	seen    bool
	err     error
}

func (collector *proxyAudioSampleCollector) Write(data []byte) (int, error) {
	if collector.err != nil {
		return len(data), nil
	}
	collector.buffer = append(collector.buffer, data...)
	if len(collector.buffer) > 128 && !strings.ContainsRune(string(collector.buffer), '\n') {
		collector.reject()
		return len(data), nil
	}
	for collector.err == nil {
		index := strings.IndexByte(string(collector.buffer), '\n')
		if index < 0 {
			break
		}
		collector.consume(collector.buffer[:index])
		collector.buffer = collector.buffer[index+1:]
	}
	return len(data), nil
}

func (collector *proxyAudioSampleCollector) consume(line []byte) {
	fields := strings.Split(strings.TrimSpace(string(line)), ",")
	if len(fields) != 2 || fields[0] != "s16" {
		collector.reject()
		return
	}
	count, err := strconv.ParseUint(fields[1], 10, 32)
	if err != nil || count == 0 || count > collector.maximum-collector.total {
		collector.reject()
		return
	}
	collector.total += count
	collector.seen = true
}

func (collector *proxyAudioSampleCollector) reject() {
	collector.err = domain.ErrInvalidMediaFacts
	if collector.stop != nil {
		collector.stop()
	}
}

func (collector *proxyAudioSampleCollector) Finish() error {
	if collector.err == nil && len(collector.buffer) > 0 {
		collector.consume(collector.buffer)
	}
	if collector.err != nil || !collector.seen || collector.total == 0 || collector.total > collector.maximum {
		return domain.ErrInvalidMediaFacts
	}
	return nil
}
