package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
	"github.com/PerishCode/open-cut/utils/atomicfile"
)

type ProductResourceDownloader struct {
	client *http.Client
	root   string
	mu     sync.Mutex
}

type productResourcePartial struct {
	Schema      int           `json:"schema"`
	EntryDigest domain.Digest `json:"entryDigest"`
	Origin      string        `json:"origin"`
	ByteSize    domain.UInt64 `json:"byteSize"`
	SHA256      domain.Digest `json:"sha256"`
	StrongETag  string        `json:"strongETag,omitempty"`
}

func NewProductResourceDownloader(client *http.Client, root string) (*ProductResourceDownloader, error) {
	if !cleanAbsolute(root) {
		return nil, fmt.Errorf("product resource download root is invalid")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	if client == nil {
		transport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, fmt.Errorf("default HTTP transport is unavailable")
		}
		client = &http.Client{Transport: transport.Clone()}
	} else {
		clone := *client
		client = &clone
	}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > 3 || !safeProductResourceRedirect(request.URL) {
			return errors.New("product resource redirect is invalid")
		}
		return nil
	}
	return &ProductResourceDownloader{client: client, root: root}, nil
}

func safeProductResourceRedirect(value *url.URL) bool {
	return value != nil && value.Scheme == "https" && value.Host != "" && value.User == nil && value.Fragment == ""
}

func (downloader *ProductResourceDownloader) Version() string {
	return application.ProductResourceDownloaderV1
}

func (downloader *ProductResourceDownloader) Download(
	ctx context.Context,
	claim application.ProductResourceJobClaim,
) (application.ProductResourceDownload, error) {
	if _, err := domain.ParseRequestID(claim.InstallationID); err != nil {
		return application.ProductResourceDownload{}, application.NewProductResourceDownloadError(
			"resource-input-invalid", err,
		)
	}
	canonical, digest, err := application.CanonicalProductResourceCatalogEntry(claim.Entry)
	if err != nil || digest != claim.Entry.EntryDigest || string(canonical) != string(claim.Entry.Canonical) {
		return application.ProductResourceDownload{}, application.NewProductResourceDownloadError(
			"resource-input-invalid", application.ErrProductResourceInvalid,
		)
	}
	downloader.mu.Lock()
	defer downloader.mu.Unlock()
	key := strings.TrimPrefix(claim.Entry.EntryDigest.String(), "sha256:")
	partialPath := filepath.Join(downloader.root, key+".partial")
	metadataPath := filepath.Join(downloader.root, key+".json")
	metadata, size, err := downloader.loadPartial(partialPath, metadataPath, claim.Entry)
	if err != nil {
		return application.ProductResourceDownload{}, application.NewProductResourceDownloadError(
			"resource-staging-invalid", err,
		)
	}
	if size == int64(claim.Entry.ByteSize.Value()) {
		if err := verifyDownloadedProductResource(partialPath, claim.Entry); err == nil {
			return preparedProductResourceDownload(partialPath, metadataPath, claim.Entry), nil
		}
		if err := resetProductResourcePartial(partialPath, metadataPath); err != nil {
			return application.ProductResourceDownload{}, err
		}
		metadata, size = productResourcePartial{}, 0
	}
	if size > 0 && metadata.StrongETag != "" {
		resumed, err := downloader.request(ctx, claim.Entry, metadata, partialPath, metadataPath, size)
		if err != nil {
			return application.ProductResourceDownload{}, err
		}
		if resumed {
			return downloader.finish(partialPath, metadataPath, claim.Entry)
		}
		if err := resetProductResourcePartial(partialPath, metadataPath); err != nil {
			return application.ProductResourceDownload{}, err
		}
	} else if size > 0 {
		if err := resetProductResourcePartial(partialPath, metadataPath); err != nil {
			return application.ProductResourceDownload{}, err
		}
	}
	completed, err := downloader.request(
		ctx, claim.Entry, productResourcePartial{}, partialPath, metadataPath, 0,
	)
	if err != nil {
		return application.ProductResourceDownload{}, err
	}
	if !completed {
		return application.ProductResourceDownload{}, application.NewProductResourceDownloadError(
			"resource-response-invalid", errors.New("resource origin did not provide a complete representation"),
		)
	}
	return downloader.finish(partialPath, metadataPath, claim.Entry)
}

func (downloader *ProductResourceDownloader) loadPartial(
	partialPath, metadataPath string,
	entry application.ProductResourceCatalogEntry,
) (productResourcePartial, int64, error) {
	info, err := os.Lstat(partialPath)
	if errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(metadataPath)
		return productResourcePartial{}, 0, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() < 0 || uint64(info.Size()) > entry.ByteSize.Value() {
		if resetErr := resetProductResourcePartial(partialPath, metadataPath); resetErr != nil {
			return productResourcePartial{}, 0, resetErr
		}
		return productResourcePartial{}, 0, nil
	}
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		if resetErr := resetProductResourcePartial(partialPath, metadataPath); resetErr != nil {
			return productResourcePartial{}, 0, resetErr
		}
		return productResourcePartial{}, 0, nil
	}
	var metadata productResourcePartial
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		metadata.Schema != 1 || metadata.EntryDigest != entry.EntryDigest || metadata.Origin != entry.Origin ||
		metadata.ByteSize != entry.ByteSize || metadata.SHA256 != entry.SHA256 ||
		(metadata.StrongETag != "" && !strongETag(metadata.StrongETag)) {
		if resetErr := resetProductResourcePartial(partialPath, metadataPath); resetErr != nil {
			return productResourcePartial{}, 0, resetErr
		}
		return productResourcePartial{}, 0, nil
	}
	return metadata, info.Size(), nil
}

func (downloader *ProductResourceDownloader) request(
	ctx context.Context,
	entry application.ProductResourceCatalogEntry,
	metadata productResourcePartial,
	partialPath, metadataPath string,
	start int64,
) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.Origin, nil)
	if err != nil {
		return false, application.NewProductResourceDownloadError("resource-request-invalid", err)
	}
	request.Header.Set("Accept-Encoding", "identity")
	if start > 0 {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
		request.Header.Set("If-Range", metadata.StrongETag)
	}
	response, err := downloader.client.Do(request)
	if err != nil {
		return false, application.NewProductResourceDownloadError("resource-network-failed", err)
	}
	defer response.Body.Close()
	if start > 0 {
		expectedRange := fmt.Sprintf("bytes %d-%d/%d", start, entry.ByteSize.Value()-1, entry.ByteSize.Value())
		if response.StatusCode != http.StatusPartialContent || response.ContentLength != int64(entry.ByteSize.Value())-start ||
			response.Header.Get("Content-Range") != expectedRange ||
			response.Header.Get("ETag") != metadata.StrongETag {
			return false, nil
		}
	} else if response.StatusCode != http.StatusOK || response.ContentLength != int64(entry.ByteSize.Value()) {
		return false, application.NewProductResourceDownloadError(
			"resource-response-invalid", fmt.Errorf("resource origin returned %s", response.Status),
		)
	}
	if encoding := response.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return false, application.NewProductResourceDownloadError(
			"resource-response-invalid", errors.New("resource origin applied content encoding"),
		)
	}
	if start == 0 {
		metadata = productResourcePartial{
			Schema: 1, EntryDigest: entry.EntryDigest, Origin: entry.Origin,
			ByteSize: entry.ByteSize, SHA256: entry.SHA256,
		}
		if etag := response.Header.Get("ETag"); strongETag(etag) {
			metadata.StrongETag = etag
		}
		if err := atomicfile.WriteJSON(metadataPath, metadata, 0o600); err != nil {
			return false, err
		}
	}
	flags := os.O_CREATE | os.O_WRONLY
	if start == 0 {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_APPEND
	}
	output, err := os.OpenFile(partialPath, flags, 0o600)
	if err != nil {
		return false, err
	}
	remaining := int64(entry.ByteSize.Value()) - start
	written, copyErr := io.Copy(output, io.LimitReader(response.Body, remaining+1))
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		return false, application.NewProductResourceDownloadError(
			"resource-write-failed", errors.Join(copyErr, syncErr, closeErr),
		)
	}
	if written != remaining {
		return false, application.NewProductResourceDownloadError(
			"resource-response-invalid", fmt.Errorf("resource response length was %d, expected %d", written, remaining),
		)
	}
	return true, nil
}

func (downloader *ProductResourceDownloader) finish(
	partialPath, metadataPath string,
	entry application.ProductResourceCatalogEntry,
) (application.ProductResourceDownload, error) {
	if err := verifyDownloadedProductResource(partialPath, entry); err != nil {
		_ = resetProductResourcePartial(partialPath, metadataPath)
		return application.ProductResourceDownload{}, application.NewProductResourceDownloadError(
			"resource-integrity-invalid", err,
		)
	}
	return preparedProductResourceDownload(partialPath, metadataPath, entry), nil
}

func verifyDownloadedProductResource(
	path string,
	entry application.ProductResourceCatalogEntry,
) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(hash, io.LimitReader(file, int64(entry.ByteSize.Value())+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written != int64(entry.ByteSize.Value()) ||
		"sha256:"+hex.EncodeToString(hash.Sum(nil)) != entry.SHA256.String() {
		return errors.Join(copyErr, closeErr, errors.New("downloaded product resource does not match its catalog entry"))
	}
	return nil
}

func preparedProductResourceDownload(
	partialPath, metadataPath string,
	entry application.ProductResourceCatalogEntry,
) application.ProductResourceDownload {
	return application.ProductResourceDownload{
		ByteSize: entry.ByteSize, SHA256: entry.SHA256,
		Workspace: &productResourceWorkspace{content: partialPath, metadata: metadataPath},
	}
}

func resetProductResourcePartial(partialPath, metadataPath string) error {
	return errors.Join(removeProductResourcePartial(partialPath), removeProductResourcePartial(metadataPath))
}

func removeProductResourcePartial(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func strongETag(value string) bool {
	return len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' && !strings.HasPrefix(value, "W/")
}

type productResourceWorkspace struct {
	content  string
	metadata string
	mu       sync.Mutex
	released bool
}

func (workspace *productResourceWorkspace) Open() (io.ReadCloser, error) {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.released {
		return nil, fmt.Errorf("product resource workspace was released")
	}
	info, err := os.Lstat(workspace.content)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("product resource workspace is invalid")
	}
	return os.Open(workspace.content)
}

func (workspace *productResourceWorkspace) Release() error {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.released {
		return nil
	}
	workspace.released = true
	return resetProductResourcePartial(workspace.content, workspace.metadata)
}

var _ application.ProductResourceDownloader = (*ProductResourceDownloader)(nil)
var _ application.PreparedProductResource = (*productResourceWorkspace)(nil)
