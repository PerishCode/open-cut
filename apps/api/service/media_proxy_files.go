package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func inspectProxyFile(path, relative, mime string) (application.SourceProxyArtifactFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return application.SourceProxyArtifactFile{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 ||
		uint64(info.Size()) > application.MaximumSourceProxyArtifactSize {
		return application.SourceProxyArtifactFile{}, domain.ErrInvalidMediaFacts
	}
	digest := sha256.New()
	written, err := io.Copy(digest, file)
	if err != nil || written != info.Size() {
		return application.SourceProxyArtifactFile{}, domain.ErrInvalidMediaFacts
	}
	size, err := domain.NewUInt64(uint64(written))
	if err != nil {
		return application.SourceProxyArtifactFile{}, err
	}
	return application.SourceProxyArtifactFile{
		Path: relative, MimeType: mime, ByteSize: size,
		SHA256: domain.Digest("sha256:" + hex.EncodeToString(digest.Sum(nil))),
	}, nil
}

func writeProxyTimeMap(
	root string,
	sourcePTS []int64,
	proxyPTS []int64,
) (application.SourceProxyArtifactFile, error) {
	path := filepath.Join(root, "video-time-map.bin")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return application.SourceProxyArtifactFile{}, err
	}
	digest := sha256.New()
	err = application.WriteSourceProxyTimeMap(io.MultiWriter(file, digest), sourcePTS, proxyPTS)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return application.SourceProxyArtifactFile{}, err
	}
	if closeErr != nil {
		return application.SourceProxyArtifactFile{}, closeErr
	}
	size, err := domain.NewUInt64(uint64(16 + len(sourcePTS)*16))
	if err != nil {
		return application.SourceProxyArtifactFile{}, err
	}
	return application.SourceProxyArtifactFile{
		Path: "video-time-map.bin", MimeType: "application/vnd.open-cut.pts-map",
		ByteSize: size, SHA256: domain.Digest("sha256:" + hex.EncodeToString(digest.Sum(nil))),
	}, nil
}

type proxyWorkspace struct {
	root     string
	allowed  map[string]struct{}
	mu       sync.Mutex
	released bool
}

func newProxyWorkspace(root string, hasVideo bool) *proxyWorkspace {
	allowed := map[string]struct{}{"proxy.webm": {}}
	if hasVideo {
		allowed["video-time-map.bin"] = struct{}{}
	}
	return &proxyWorkspace{root: root, allowed: allowed}
}

func (workspace *proxyWorkspace) Open(relativePath string) (io.ReadCloser, error) {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.released {
		return nil, fmt.Errorf("proxy workspace was released")
	}
	if _, allowed := workspace.allowed[relativePath]; !allowed || filepath.Base(relativePath) != relativePath {
		return nil, fmt.Errorf("proxy workspace file is unavailable")
	}
	path := filepath.Join(workspace.root, relativePath)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("proxy workspace file is invalid")
	}
	return os.Open(path)
}

func (workspace *proxyWorkspace) Release() error {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.released {
		return nil
	}
	workspace.released = true
	return os.RemoveAll(workspace.root)
}

var _ application.PreparedMediaWorkspace = (*proxyWorkspace)(nil)
