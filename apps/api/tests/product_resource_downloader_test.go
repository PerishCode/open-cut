package tests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PerishCode/open-cut/apps/api/service"
	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func TestProductResourceDownloaderPublishesOnlyExactAuthenticatedBytes(t *testing.T) {
	parallelAPITest(t)
	content := []byte("authenticated whisper model fixture")
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/model.bin" || request.Header.Get("Accept-Encoding") != "identity" {
			t.Fatalf("request=%s headers=%v", request.URL.Path, request.Header)
		}
		writer.Header().Set("Content-Length", domain.NewInt64(int64(len(content))).String())
		writer.Header().Set("ETag", `"fixture-v1"`)
		_, _ = writer.Write(content)
	}))
	defer server.Close()
	entry := downloaderProductResourceEntry(t, server.URL+"/model.bin", content)
	root := t.TempDir()
	downloader, err := service.NewProductResourceDownloader(server.Client(), root)
	if err != nil {
		t.Fatal(err)
	}
	download, err := downloader.Download(context.Background(), application.ProductResourceJobClaim{
		InstallationID: "installation-test", Entry: entry,
	})
	if err != nil {
		t.Fatal(err)
	}
	file, err := download.Workspace.Open()
	if err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(file)
	file.Close()
	if err != nil || string(actual) != string(content) || download.ByteSize != entry.ByteSize ||
		download.SHA256 != entry.SHA256 {
		t.Fatalf("download=%+v bytes=%q err=%v", download, actual, err)
	}
	if err := download.Workspace.Release(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging entries=%v err=%v", entries, err)
	}
}

func TestProductResourceDownloaderResumesOnlyAgainstTheSameStrongRepresentation(t *testing.T) {
	parallelAPITest(t)
	content := []byte("0123456789abcdef")
	const split = 6
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		writer.Header().Set("ETag", `"fixture-v1"`)
		if requests == 1 {
			writer.Header().Set("Content-Length", "16")
			_, _ = writer.Write(content[:split])
			return
		}
		if request.Header.Get("Range") != "bytes=6-" || request.Header.Get("If-Range") != `"fixture-v1"` {
			t.Fatalf("resume headers=%v", request.Header)
		}
		writer.Header().Set("Content-Length", "10")
		writer.Header().Set("Content-Range", "bytes 6-15/16")
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = writer.Write(content[split:])
	}))
	defer server.Close()
	entry := downloaderProductResourceEntry(t, server.URL+"/model.bin", content)
	downloader, err := service.NewProductResourceDownloader(server.Client(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	claim := application.ProductResourceJobClaim{InstallationID: "installation-test", Entry: entry}
	if _, err := downloader.Download(context.Background(), claim); err == nil {
		t.Fatal("truncated first response unexpectedly succeeded")
	}
	download, err := downloader.Download(context.Background(), claim)
	if err != nil || requests != 2 {
		t.Fatalf("requests=%d err=%v", requests, err)
	}
	if err := download.Workspace.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestProductResourceDownloaderRejectsDigestMismatchAndDeletesPoisonedStage(t *testing.T) {
	parallelAPITest(t)
	expected := []byte("expected")
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", "8")
		_, _ = writer.Write([]byte("poisoned"))
	}))
	defer server.Close()
	entry := downloaderProductResourceEntry(t, server.URL+"/model.bin", expected)
	root := t.TempDir()
	downloader, err := service.NewProductResourceDownloader(server.Client(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := downloader.Download(context.Background(), application.ProductResourceJobClaim{
		InstallationID: "installation-test", Entry: entry,
	}); err == nil || !strings.Contains(err.Error(), "resource-integrity-invalid") {
		t.Fatalf("integrity error=%v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("poisoned stage retained: %v err=%v", entries, err)
	}
}

func TestProductResourceDownloaderFollowsOnlyBoundedHTTPSRedirects(t *testing.T) {
	parallelAPITest(t)
	content := []byte("redirected authenticated model")
	final := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Accept-Encoding") != "identity" {
			t.Fatalf("redirect lost representation request headers: %v", request.Header)
		}
		writer.Header().Set("Content-Length", domain.NewInt64(int64(len(content))).String())
		_, _ = writer.Write(content)
	}))
	defer final.Close()
	origin := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Redirect(writer, &http.Request{}, final.URL+"/model.bin", http.StatusFound)
	}))
	defer origin.Close()
	client := origin.Client()
	client.Transport = final.Client().Transport
	downloader, err := service.NewProductResourceDownloader(client, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	download, err := downloader.Download(context.Background(), application.ProductResourceJobClaim{
		InstallationID: "installation-test", Entry: downloaderProductResourceEntry(t, origin.URL+"/model.bin", content),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := download.Workspace.Release(); err != nil {
		t.Fatal(err)
	}

	insecure := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Location", "http://example.invalid/model.bin")
		writer.WriteHeader(http.StatusFound)
	}))
	defer insecure.Close()
	rejected, err := service.NewProductResourceDownloader(insecure.Client(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = rejected.Download(context.Background(), application.ProductResourceJobClaim{
		InstallationID: "installation-test", Entry: downloaderProductResourceEntry(t, insecure.URL+"/model.bin", content),
	})
	if err == nil || !strings.Contains(err.Error(), "resource-network-failed") {
		t.Fatalf("insecure redirect err=%v", err)
	}
}

func downloaderProductResourceEntry(
	t *testing.T,
	origin string,
	content []byte,
) application.ProductResourceCatalogEntry {
	t.Helper()
	hash := sha256.Sum256(content)
	size, _ := domain.NewUInt64(uint64(len(content)))
	entry, err := application.NewProductResourceCatalogEntry(
		application.TranscriptProfile, domain.ProductResourceTranscriptionModel,
		"fixture-v1", application.TranscriptProfile, origin, size,
		domain.Digest("sha256:"+hex.EncodeToString(hash[:])), domain.ProductResourceRetentionOffline,
	)
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

// A CI cache restores the downloader's staging directory rather than the
// published resource, so the reuse it depends on is this one: a full-size
// staged file is re-verified against the pinned digest and served without
// touching the network. Nothing about the cache is trusted - a restored file
// that does not match costs a download, which the sibling digest-mismatch test
// already pins.
func TestProductResourceDownloaderReusesAVerifiedFullStageWithoutTheNetwork(t *testing.T) {
	parallelAPITest(t)
	content := []byte("authenticated whisper model fixture")
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Length", domain.NewInt64(int64(len(content))).String())
		_, _ = writer.Write(content)
	}))
	defer server.Close()
	entry := downloaderProductResourceEntry(t, server.URL+"/model.bin", content)
	root := t.TempDir()
	// A restored cache carries both files the downloader stages. The metadata
	// is not decoration: without it the partial is discarded and re-fetched,
	// which is exactly the failure this test exists to catch.
	key := strings.TrimPrefix(entry.EntryDigest.String(), "sha256:")
	if err := os.WriteFile(filepath.Join(root, key+".partial"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	metadata, err := json.Marshal(map[string]any{
		"schema": 1, "entryDigest": entry.EntryDigest.String(), "origin": entry.Origin,
		"byteSize": entry.ByteSize.String(), "sha256": entry.SHA256.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, key+".json"), metadata, 0o600); err != nil {
		t.Fatal(err)
	}
	downloader, downloaderErr := service.NewProductResourceDownloader(server.Client(), root)
	if downloaderErr != nil {
		t.Fatal(downloaderErr)
	}
	download, err := downloader.Download(context.Background(), application.ProductResourceJobClaim{
		InstallationID: "installation-test", Entry: entry,
	})
	if err != nil {
		t.Fatal(err)
	}
	if download.ByteSize != entry.ByteSize || download.SHA256 != entry.SHA256 {
		t.Fatalf("download=%+v", download)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("a verified full stage still made %d request(s)", got)
	}
	if err := download.Workspace.Release(); err != nil {
		t.Fatal(err)
	}
}
