package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type TranscriptResourceRepository interface {
	ReadBoundTranscriptResource(
		context.Context,
		application.MediaJobClaim,
		time.Time,
	) (domain.ProductResource, error)
}

type TranscriptResourceAccess struct {
	repository TranscriptResourceRepository
	root       string
	clock      application.Clock
}

func NewTranscriptResourceAccess(
	repository TranscriptResourceRepository,
	dataDir string,
	clock application.Clock,
) (*TranscriptResourceAccess, error) {
	if repository == nil || clock == nil || !cleanAbsolute(dataDir) {
		return nil, fmt.Errorf("transcript resource access configuration is invalid")
	}
	root := filepath.Join(dataDir, "resources", "product")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &TranscriptResourceAccess{repository: repository, root: root, clock: clock}, nil
}

func (access *TranscriptResourceAccess) Resolve(
	ctx context.Context,
	claim application.MediaJobClaim,
) (string, error) {
	resource, err := access.repository.ReadBoundTranscriptResource(ctx, claim, access.clock.Now().UTC())
	if err != nil {
		return "", err
	}
	resourceRoot := filepath.Join(access.root, resource.ID.String())
	contentPath := filepath.Join(resourceRoot, "content.bin")
	if !pathWithin(access.root, resourceRoot) || !pathWithin(resourceRoot, contentPath) {
		return "", application.NewMediaResourceInvalidError(
			resource.ID, fmt.Errorf("bound resource path escaped resource custody"),
		)
	}
	rootInfo, rootErr := os.Lstat(resourceRoot)
	contentInfo, contentErr := os.Lstat(contentPath)
	if rootErr != nil || contentErr != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 ||
		!contentInfo.Mode().IsRegular() || contentInfo.Mode()&os.ModeSymlink != 0 ||
		contentInfo.Size() < 0 || uint64(contentInfo.Size()) != resource.ByteSize.Value() {
		return "", application.NewMediaResourceInvalidError(
			resource.ID, fmt.Errorf("bound resource structure is invalid"),
		)
	}
	file, err := os.Open(contentPath)
	if err != nil {
		return "", application.NewMediaResourceInvalidError(resource.ID, err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(hash, io.LimitReader(file, int64(resource.ByteSize.Value())+1))
	openedInfo, statErr := file.Stat()
	closeErr := file.Close()
	actualDigest := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if copyErr != nil || statErr != nil || closeErr != nil || !openedInfo.Mode().IsRegular() ||
		openedInfo.Size() < 0 || written != int64(resource.ByteSize.Value()) ||
		uint64(openedInfo.Size()) != resource.ByteSize.Value() || actualDigest != resource.ContentDigest.String() {
		return "", application.NewMediaResourceInvalidError(
			resource.ID, fmt.Errorf("bound resource content digest is invalid"),
		)
	}
	return contentPath, nil
}
