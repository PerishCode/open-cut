package mediatoolchain

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// pinnedSourceAttempts bounds how often a pinned source download is retried.
// Every byte is verified against the pinned digest, so retrying a transient
// transport failure cannot weaken the supply chain, and a cold toolchain build
// costs half an hour - too much to discard because one upstream host answered
// 503 once. Deterministic answers are never retried: a missing pin or a digest
// mismatch is a real failure, and retrying would only hide it behind a longer
// build.
const pinnedSourceAttempts = 3

// pinnedSourceRetryDelay is the first backoff between download attempts.
// Variable only so tests can exercise the retry policy without spending the
// wall time it describes: a test that really waits is a test that makes the
// suite it lives in too slow to run often.
var pinnedSourceRetryDelay = 2 * time.Second

func ensureSource(ctx context.Context, archive string, source SourceRecord) error {
	if digest, _, err := digestFile(archive); err == nil && digest == source.SHA256 {
		return nil
	}
	delay := pinnedSourceRetryDelay
	for attempt := 1; ; attempt++ {
		err := fetchSource(ctx, archive, source)
		if err == nil {
			return nil
		}
		var transient transientSourceError
		if !errors.As(err, &transient) || attempt >= pinnedSourceAttempts {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(delay):
		}
		delay *= 2
	}
}

// transientSourceError marks a download failure that a later attempt may
// legitimately resolve.
type transientSourceError struct{ cause error }

func (err transientSourceError) Error() string { return err.cause.Error() }
func (err transientSourceError) Unwrap() error { return err.cause }

func fetchSource(ctx context.Context, archive string, source SourceRecord) error {
	if err := os.MkdirAll(filepath.Dir(archive), 0o700); err != nil {
		return fmt.Errorf("create media source cache: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(archive), ".media-source-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		temporary.Close()
		return err
	}
	request.Header.Set("User-Agent", "open-cut-media-toolchain/1")
	client := &http.Client{Timeout: 5 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		temporary.Close()
		return transientSourceError{cause: fmt.Errorf("download pinned %s source from %s: %w", source.ID, requestHost(source.URL), err)}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		temporary.Close()
		failure := fmt.Errorf(
			"download pinned %s source from %s: HTTP %d", source.ID, requestHost(source.URL), response.StatusCode,
		)
		if retryableStatus(response.StatusCode) {
			return transientSourceError{cause: failure}
		}
		return failure
	}
	if response.ContentLength > maximumSourceBytes {
		temporary.Close()
		return fmt.Errorf(
			"download pinned %s source from %s: content length %d exceeds the %d byte maximum",
			source.ID, requestHost(source.URL), response.ContentLength, maximumSourceBytes,
		)
	}
	written, copyErr := io.Copy(temporary, io.LimitReader(response.Body, maximumSourceBytes+1))
	if copyErr != nil {
		temporary.Close()
		return transientSourceError{cause: fmt.Errorf(
			"download pinned %s source from %s: %w", source.ID, requestHost(source.URL), copyErr,
		)}
	}
	if written <= 0 || written > maximumSourceBytes {
		temporary.Close()
		return fmt.Errorf(
			"download pinned %s source from %s: body is %d bytes", source.ID, requestHost(source.URL), written,
		)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	digest, _, err := digestFile(temporaryPath)
	if err != nil || digest != source.SHA256 {
		return fmt.Errorf("pinned %s source digest mismatch", source.ID)
	}
	if err := os.Rename(temporaryPath, archive); err != nil {
		return fmt.Errorf("publish media source cache: %w", err)
	}
	return nil
}

// retryableStatus reports whether an HTTP status is a transient upstream
// answer. A 404 or 410 means the pin points at something that is not there,
// which no retry can fix.
func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500
}

// requestHost names the upstream a pinned download addressed, so a build log
// says which host answered instead of only which source failed.
func requestHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return "an unparsable origin"
	}
	return parsed.Host
}
