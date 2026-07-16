package renderhelper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"

	"github.com/PerishCode/open-cut/internal/renderengine"
	"github.com/PerishCode/open-cut/product/domain"
)

type streamObservation struct {
	hash    hash.Hash
	written uint64
	closed  bool
}

func observeStream(
	producer renderengine.StreamProducer,
) (renderengine.StreamProducer, *streamObservation, error) {
	if producer == nil {
		return nil, nil, fmt.Errorf("render stream producer is unavailable")
	}
	observation := &streamObservation{hash: sha256.New()}
	wrapped := func(ctx context.Context, destination io.Writer) error {
		if observation.closed {
			return fmt.Errorf("render stream producer was reused")
		}
		observation.closed = true
		return producer(ctx, &observingWriter{destination: destination, observation: observation})
	}
	return wrapped, observation, nil
}

func (observation *streamObservation) result() (renderengine.ResultStreamObservation, error) {
	if observation == nil || !observation.closed || observation.hash == nil || observation.written == 0 {
		return renderengine.ResultStreamObservation{}, fmt.Errorf("render stream observation is incomplete")
	}
	size, err := domain.NewUInt64(observation.written)
	if err != nil {
		return renderengine.ResultStreamObservation{}, err
	}
	return renderengine.ResultStreamObservation{
		ByteSize: size,
		SHA256:   domain.Digest("sha256:" + hex.EncodeToString(observation.hash.Sum(nil))),
	}, nil
}

type observingWriter struct {
	destination io.Writer
	observation *streamObservation
}

func (writer *observingWriter) Write(value []byte) (int, error) {
	written, err := writer.destination.Write(value)
	if written > 0 {
		_, _ = writer.observation.hash.Write(value[:written])
		writer.observation.written += uint64(written)
	}
	return written, err
}
