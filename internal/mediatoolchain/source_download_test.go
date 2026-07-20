package mediatoolchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fastRetries keeps the retry policy under test without paying its wall time.
func fastRetries(t *testing.T) {
	t.Helper()
	previous := pinnedSourceRetryDelay
	pinnedSourceRetryDelay = time.Millisecond
	t.Cleanup(func() { pinnedSourceRetryDelay = previous })
}

func pinnedSource(t *testing.T, origin string, body []byte) SourceRecord {
	t.Helper()
	fastRetries(t)
	digest := sha256.Sum256(body)
	return SourceRecord{
		ID: "freetype", URL: origin + "/freetype.tar.gz",
		SHA256: "sha256:" + hex.EncodeToString(digest[:]),
	}
}

func TestPinnedSourceDownloadRetriesTransientUpstreamAnswers(t *testing.T) {
	body := []byte("pinned source bytes")
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = writer.Write(body)
	}))
	defer server.Close()

	archive := filepath.Join(t.TempDir(), "freetype.tar.gz")
	if err := ensureSource(context.Background(), archive, pinnedSource(t, server.URL, body)); err != nil {
		t.Fatalf("transient 503 was not retried: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
}

func TestPinnedSourceDownloadDoesNotRetryAMissingPin(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	archive := filepath.Join(t.TempDir(), "freetype.tar.gz")
	err := ensureSource(context.Background(), archive, pinnedSource(t, server.URL, []byte("unused")))
	if err == nil {
		t.Fatal("a missing pin was accepted")
	}
	// A deterministic answer must fail on the first attempt and say why.
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
	for _, fragment := range []string{"freetype", "HTTP 404", "127.0.0.1"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("error %q omits %q", err, fragment)
		}
	}
}

func TestPinnedSourceDownloadRejectsADigestMismatchWithoutRetrying(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		_, _ = writer.Write([]byte("substituted bytes"))
	}))
	defer server.Close()

	archive := filepath.Join(t.TempDir(), "freetype.tar.gz")
	err := ensureSource(context.Background(), archive, pinnedSource(t, server.URL, []byte("pinned source bytes")))
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("digest mismatch error = %v", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func TestPinnedSourceDownloadGivesUpAfterBoundedTransientFailures(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		writer.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	archive := filepath.Join(t.TempDir(), "freetype.tar.gz")
	err := ensureSource(context.Background(), archive, pinnedSource(t, server.URL, []byte("unused")))
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", http.StatusBadGateway)) {
		t.Fatalf("bounded retry error = %v", err)
	}
	if got := attempts.Load(); got != pinnedSourceAttempts {
		t.Fatalf("attempts = %d, want %d", got, pinnedSourceAttempts)
	}
}
