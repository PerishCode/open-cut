package renderengine

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/PerishCode/open-cut/lifecycle"
)

type StreamProducer func(context.Context, io.Writer) error

// RunBoundedProcessStream connects one product-owned byte producer to a
// contained child stdin with kernel backpressure. No raw payload is buffered or
// materialized by this boundary. The caller's ProcessSpec must leave Stdin
// unset; this function is its sole owner.
func RunBoundedProcessStream(
	ctx context.Context,
	spec lifecycle.ProcessSpec,
	byteLimit uint64,
	produce StreamProducer,
) (uint64, error) {
	if ctx == nil || spec.Stdin != nil || !spec.ContainProcessTree || byteLimit == 0 || produce == nil {
		return 0, fmt.Errorf("bounded process stream configuration is invalid")
	}
	reader, writer := io.Pipe()
	spec.Stdin = reader
	process, err := lifecycle.Start(ctx, spec)
	if err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return 0, err
	}
	bounded := &boundedStreamWriter{destination: writer, limit: byteLimit}
	producerResult := make(chan error, 1)
	go func() {
		producerErr := produce(ctx, bounded)
		_ = writer.CloseWithError(producerErr)
		producerResult <- producerErr
	}()
	processErr := process.Wait()
	_ = reader.CloseWithError(processErr)
	producerErr := <-producerResult
	if ctx.Err() != nil {
		return bounded.written, ctx.Err()
	}
	if isStableProducerFailure(producerErr) {
		return bounded.written, producerErr
	}
	if processErr != nil {
		return bounded.written, processErr
	}
	if producerErr != nil {
		return bounded.written, producerErr
	}
	return bounded.written, nil
}

func isStableProducerFailure(err error) bool {
	if err == nil {
		return false
	}
	var limit ResourceLimitError
	var missing CaptionGlyphMissingError
	var color CaptionColorEmojiError
	return errors.As(err, &limit) || errors.As(err, &missing) || errors.As(err, &color)
}

type boundedStreamWriter struct {
	destination io.Writer
	limit       uint64
	written     uint64
}

func (writer *boundedStreamWriter) Write(value []byte) (int, error) {
	if uint64(len(value)) > writer.limit-writer.written {
		return 0, ResourceLimitError{Subject: "raw-stream-bytes"}
	}
	written, err := writer.destination.Write(value)
	writer.written += uint64(written)
	if err == nil && written != len(value) {
		err = io.ErrShortWrite
	}
	return written, err
}
