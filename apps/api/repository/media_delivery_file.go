package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type verifiedMediaFile struct {
	info   os.FileInfo
	digest domain.Digest
}

func (repository *SQLiteProjects) IsSequencePreviewMediaVerificationCurrent(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	sequenceRevision domain.Revision,
	planDigest domain.Digest,
	artifactID domain.ArtifactID,
) (bool, error) {
	if projectID.IsZero() || sequenceID.IsZero() || sequenceRevision.Value() == 0 ||
		planDigest == "" || artifactID.IsZero() {
		return false, application.ErrSequencePreviewInvalid
	}
	var state string
	err := repository.db.QueryRowContext(ctx, `
SELECT state FROM sequence_preview_artifacts
WHERE id = ? AND project_id = ? AND sequence_id = ? AND sequence_revision = ?
  AND render_plan_digest = ?`,
		artifactID.String(), projectID.String(), sequenceID.String(), sequenceRevision.Value(), planDigest.String(),
	).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return false, application.ErrSequencePreviewNotFound
	}
	if err != nil {
		return false, err
	}
	if state != string(domain.SequencePreviewArtifactReady) {
		return false, application.ErrSequencePreviewNotFound
	}
	path := filepath.Join(
		repository.dataDir, "artifacts", "sequence-preview", artifactID.String(), "preview.webm",
	)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, nil
	}
	prefix := path + "\x00"
	repository.mediaVerificationMu.Lock()
	defer repository.mediaVerificationMu.Unlock()
	for key, cached := range repository.mediaVerificationCache {
		if strings.HasPrefix(key, prefix) && os.SameFile(cached.info, info) &&
			cached.info.Size() == info.Size() && cached.info.ModTime().Equal(info.ModTime()) {
			return true, nil
		}
	}
	return false, nil
}

func (repository *SQLiteProjects) OpenSequencePreviewMedia(
	ctx context.Context,
	projectID domain.ProjectID,
	sequenceID domain.SequenceID,
	sequenceRevision domain.Revision,
	planDigest domain.Digest,
	artifactID domain.ArtifactID,
) (*os.File, application.SequencePreviewArtifactFile, error) {
	if projectID.IsZero() || sequenceID.IsZero() || sequenceRevision.Value() == 0 ||
		planDigest == "" || artifactID.IsZero() {
		return nil, application.SequencePreviewArtifactFile{}, application.ErrSequencePreviewInvalid
	}
	var renderer, target, profile, factsJSON, byteReference, contentDigest string
	var byteSize uint64
	err := repository.db.QueryRowContext(ctx, `
SELECT renderer_version, renderer_target, output_profile, facts_json,
       byte_reference, byte_size, content_digest
FROM sequence_preview_artifacts
WHERE id = ? AND project_id = ? AND sequence_id = ? AND sequence_revision = ?
  AND render_plan_digest = ? AND state = 'ready'`,
		artifactID.String(), projectID.String(), sequenceID.String(), sequenceRevision.Value(), planDigest.String(),
	).Scan(&renderer, &target, &profile, &factsJSON, &byteReference, &byteSize, &contentDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, application.SequencePreviewArtifactFile{}, application.ErrSequencePreviewNotFound
	}
	if err != nil {
		return nil, application.SequencePreviewArtifactFile{}, err
	}
	if byteReference != "artifact:sequence-preview/"+artifactID.String() {
		return nil, application.SequencePreviewArtifactFile{}, sequencePreviewIntegrityError(
			application.ErrSequencePreviewInvalid,
		)
	}
	root := filepath.Join(repository.dataDir, "artifacts", "sequence-preview", artifactID.String())
	if err := requireDirectory(root); err != nil {
		return nil, application.SequencePreviewArtifactFile{}, sequencePreviewIntegrityError(err)
	}
	if err := requireExactDirectoryEntries(root, []string{"manifest.json", "preview.webm"}); err != nil {
		return nil, application.SequencePreviewArtifactFile{}, sequencePreviewIntegrityError(err)
	}
	manifestBytes, err := readBoundedRegularFile(
		filepath.Join(root, "manifest.json"), application.MaximumSequencePreviewManifestSize,
	)
	if err != nil {
		return nil, application.SequencePreviewArtifactFile{}, sequencePreviewIntegrityError(err)
	}
	manifestHash := sha256.Sum256(manifestBytes)
	if contentDigest != "sha256:"+hex.EncodeToString(manifestHash[:]) {
		return nil, application.SequencePreviewArtifactFile{}, sequencePreviewIntegrityError(
			application.ErrSequencePreviewInvalid,
		)
	}
	manifest, err := application.DecodeSequencePreviewArtifactManifest(manifestBytes)
	if err != nil {
		return nil, application.SequencePreviewArtifactFile{}, sequencePreviewIntegrityError(err)
	}
	var facts domain.SequencePreviewMediaFacts
	if err := json.Unmarshal([]byte(factsJSON), &facts); err != nil ||
		application.ValidateSequencePreviewFacts(facts) != nil ||
		manifest.ProjectID != projectID || manifest.SequenceID != sequenceID ||
		manifest.SequenceRevision != sequenceRevision || manifest.RenderPlanDigest != planDigest ||
		manifest.RendererVersion != renderer || manifest.RendererTarget != target ||
		manifest.Profile != profile || manifest.Facts != facts ||
		uint64(len(manifestBytes))+manifest.Media.ByteSize.Value() != byteSize {
		return nil, application.SequencePreviewArtifactFile{}, sequencePreviewIntegrityError(
			application.ErrSequencePreviewInvalid,
		)
	}
	path := filepath.Join(root, manifest.Media.Path)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		uint64(info.Size()) != manifest.Media.ByteSize.Value() {
		return nil, application.SequencePreviewArtifactFile{}, sequencePreviewIntegrityError(
			fmt.Errorf("sequence preview media is unavailable"),
		)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, application.SequencePreviewArtifactFile{}, err
	}
	if err := repository.verifySequencePreviewMedia(file, path, manifest.Media); err != nil {
		file.Close()
		return nil, application.SequencePreviewArtifactFile{}, sequencePreviewIntegrityError(err)
	}
	return file, manifest.Media, nil
}

func sequencePreviewIntegrityError(cause error) error {
	return fmt.Errorf("%w: %v", application.ErrSequencePreviewIntegrity, cause)
}

func (repository *SQLiteProjects) verifySequencePreviewMedia(
	file *os.File,
	path string,
	media application.SequencePreviewArtifactFile,
) error {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || uint64(info.Size()) != media.ByteSize.Value() {
		return fmt.Errorf("sequence preview media is unavailable")
	}
	cacheKey := path + "\x00" + media.SHA256.String()
	repository.mediaVerificationMu.Lock()
	cached, exists := repository.mediaVerificationCache[cacheKey]
	repository.mediaVerificationMu.Unlock()
	if exists && cached.digest == media.SHA256 && os.SameFile(cached.info, info) &&
		cached.info.Size() == info.Size() && cached.info.ModTime().Equal(info.ModTime()) {
		return nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	digest := sha256.New()
	written, err := io.Copy(digest, file)
	if err != nil || written != info.Size() ||
		"sha256:"+hex.EncodeToString(digest.Sum(nil)) != media.SHA256.String() {
		return application.ErrSequencePreviewInvalid
	}
	current, err := os.Lstat(path)
	if err != nil || current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() ||
		!os.SameFile(info, current) || current.Size() != info.Size() || !current.ModTime().Equal(info.ModTime()) {
		return fmt.Errorf("sequence preview media changed while it was verified")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	repository.mediaVerificationMu.Lock()
	if repository.mediaVerificationCache == nil {
		repository.mediaVerificationCache = make(map[string]verifiedMediaFile)
	}
	repository.mediaVerificationCache[cacheKey] = verifiedMediaFile{info: current, digest: media.SHA256}
	repository.mediaVerificationMu.Unlock()
	return nil
}

func (repository *SQLiteProjects) IsSourceProxyMediaVerificationCurrent(
	ctx context.Context,
	projectID domain.ProjectID,
	assetID domain.AssetID,
	artifactID domain.ArtifactID,
) (bool, error) {
	if projectID.IsZero() || assetID.IsZero() || artifactID.IsZero() {
		return false, application.ErrAssetInvalid
	}
	var state string
	err := repository.db.QueryRowContext(ctx, `
SELECT state FROM media_artifacts
WHERE id = ? AND project_id = ? AND asset_id = ? AND kind = 'proxy'`,
		artifactID.String(), projectID.String(), assetID.String(),
	).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return false, application.ErrAssetNotFound
	}
	if err != nil {
		return false, err
	}
	if state != string(domain.ArtifactReady) {
		return false, application.ErrAssetNotFound
	}
	path := filepath.Join(repository.dataDir, "artifacts", "media", artifactID.String(), "proxy.webm")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, nil
	}
	prefix := path + "\x00"
	repository.mediaVerificationMu.Lock()
	defer repository.mediaVerificationMu.Unlock()
	for key, cached := range repository.mediaVerificationCache {
		if strings.HasPrefix(key, prefix) && os.SameFile(cached.info, info) &&
			cached.info.Size() == info.Size() && cached.info.ModTime().Equal(info.ModTime()) {
			return true, nil
		}
	}
	return false, nil
}

func (repository *SQLiteProjects) OpenSourceProxyMedia(
	ctx context.Context,
	projectID domain.ProjectID,
	assetID domain.AssetID,
	artifactID domain.ArtifactID,
) (*os.File, application.SourceProxyArtifactManifest, error) {
	if projectID.IsZero() || assetID.IsZero() || artifactID.IsZero() {
		return nil, application.SourceProxyArtifactManifest{}, application.ErrAssetInvalid
	}
	var producer, fingerprint, parametersDigest, parametersJSON, byteReference, contentDigest string
	var byteSize uint64
	err := repository.db.QueryRowContext(ctx, `
SELECT producer_version, input_fingerprint, parameters_digest, parameters_json,
       byte_reference, byte_size, content_digest
FROM media_artifacts
WHERE id = ? AND project_id = ? AND asset_id = ? AND kind = 'proxy' AND state = 'ready'`,
		artifactID.String(), projectID.String(), assetID.String(),
	).Scan(
		&producer, &fingerprint, &parametersDigest, &parametersJSON,
		&byteReference, &byteSize, &contentDigest,
	)
	if err == sql.ErrNoRows {
		return nil, application.SourceProxyArtifactManifest{}, application.ErrAssetNotFound
	}
	if err != nil {
		return nil, application.SourceProxyArtifactManifest{}, err
	}
	if byteReference != "artifact:media/"+artifactID.String() {
		return nil, application.SourceProxyArtifactManifest{}, application.ErrAssetInvalid
	}
	root := filepath.Join(repository.dataDir, "artifacts", "media", artifactID.String())
	manifestBytes, err := readBoundedRegularFile(
		filepath.Join(root, "manifest.json"), application.MaximumSourceProxyManifestSize,
	)
	if err != nil {
		return nil, application.SourceProxyArtifactManifest{}, sourceProxyIntegrityError(err)
	}
	manifestHash := sha256.Sum256(manifestBytes)
	if contentDigest != "sha256:"+hex.EncodeToString(manifestHash[:]) {
		return nil, application.SourceProxyArtifactManifest{}, sourceProxyIntegrityError(
			fmt.Errorf("manifest digest: %w", application.ErrAssetInvalid),
		)
	}
	manifest, err := application.DecodeSourceProxyArtifactManifest(manifestBytes)
	if err != nil {
		return nil, application.SourceProxyArtifactManifest{}, sourceProxyIntegrityError(err)
	}
	parameters, err := application.DecodeInitialMediaJobParameters([]byte(parametersJSON))
	if err != nil {
		return nil, application.SourceProxyArtifactManifest{}, sourceProxyIntegrityError(err)
	}
	canonicalParameters, digest, err := application.CanonicalInitialMediaJobParameters(parameters)
	if err != nil || !bytes.Equal(canonicalParameters, []byte(parametersJSON)) ||
		digest.String() != parametersDigest || parameters.AssetID != assetID ||
		parameters.Kind != domain.MediaJobProxy || parameters.Profile != application.SourceProxyProfile ||
		manifest.AssetID != assetID || manifest.Fingerprint.String() != fingerprint ||
		manifest.Profile != parameters.Profile || manifest.Producer != producer {
		return nil, application.SourceProxyArtifactManifest{}, sourceProxyIntegrityError(
			fmt.Errorf("manifest binding: %w", application.ErrAssetInvalid),
		)
	}
	total := uint64(len(manifestBytes)) + manifest.Media.ByteSize.Value()
	if manifest.Video != nil {
		total += manifest.Video.TimeMap.ByteSize.Value()
	}
	if total != byteSize {
		return nil, application.SourceProxyArtifactManifest{}, sourceProxyIntegrityError(
			fmt.Errorf("artifact byte size: %w", application.ErrAssetInvalid),
		)
	}
	path := filepath.Join(root, filepath.FromSlash(manifest.Media.Path))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		uint64(info.Size()) != manifest.Media.ByteSize.Value() {
		return nil, application.SourceProxyArtifactManifest{}, sourceProxyIntegrityError(fmt.Errorf("source proxy media is unavailable"))
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, application.SourceProxyArtifactManifest{}, err
	}
	if err := repository.verifySourceProxyMedia(file, path, manifest.Media); err != nil {
		file.Close()
		return nil, application.SourceProxyArtifactManifest{}, sourceProxyIntegrityError(
			fmt.Errorf("media digest: %w", err),
		)
	}
	return file, manifest, nil
}

func (repository *SQLiteProjects) OpenSourceProxyVideoTimeMap(
	ctx context.Context,
	projectID domain.ProjectID,
	assetID domain.AssetID,
	artifactID domain.ArtifactID,
) (*os.File, application.SourceProxyArtifactManifest, error) {
	media, manifest, err := repository.OpenSourceProxyMedia(ctx, projectID, assetID, artifactID)
	if err != nil {
		return nil, application.SourceProxyArtifactManifest{}, err
	}
	if err := media.Close(); err != nil {
		return nil, application.SourceProxyArtifactManifest{}, err
	}
	if manifest.Video == nil {
		return nil, application.SourceProxyArtifactManifest{}, application.ErrAssetInvalid
	}
	path := filepath.Join(
		repository.dataDir, "artifacts", "media", artifactID.String(),
		filepath.FromSlash(manifest.Video.TimeMap.Path),
	)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		uint64(info.Size()) != manifest.Video.TimeMap.ByteSize.Value() {
		return nil, application.SourceProxyArtifactManifest{}, sourceProxyIntegrityError(
			fmt.Errorf("source proxy time map is unavailable"),
		)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, application.SourceProxyArtifactManifest{}, err
	}
	if err := repository.verifySourceProxyMedia(file, path, manifest.Video.TimeMap); err != nil {
		file.Close()
		return nil, application.SourceProxyArtifactManifest{}, sourceProxyIntegrityError(err)
	}
	return file, manifest, nil
}

func sourceProxyIntegrityError(cause error) error {
	return fmt.Errorf("%w: %v", application.ErrSourceProxyIntegrity, cause)
}

func (repository *SQLiteProjects) verifySourceProxyMedia(
	file *os.File,
	path string,
	media application.SourceProxyArtifactFile,
) error {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || uint64(info.Size()) != media.ByteSize.Value() {
		return fmt.Errorf("source proxy media is unavailable")
	}
	cacheKey := path + "\x00" + media.SHA256.String()
	repository.mediaVerificationMu.Lock()
	cached, exists := repository.mediaVerificationCache[cacheKey]
	repository.mediaVerificationMu.Unlock()
	if exists && cached.digest == media.SHA256 && os.SameFile(cached.info, info) &&
		cached.info.Size() == info.Size() && cached.info.ModTime().Equal(info.ModTime()) {
		return nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	digest := sha256.New()
	written, err := io.Copy(digest, file)
	if err != nil || written != info.Size() ||
		"sha256:"+hex.EncodeToString(digest.Sum(nil)) != media.SHA256.String() {
		return application.ErrAssetInvalid
	}
	current, err := os.Lstat(path)
	if err != nil || current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() ||
		!os.SameFile(info, current) || current.Size() != info.Size() || !current.ModTime().Equal(info.ModTime()) {
		return fmt.Errorf("source proxy media changed while it was verified")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	repository.mediaVerificationMu.Lock()
	if repository.mediaVerificationCache == nil {
		repository.mediaVerificationCache = make(map[string]verifiedMediaFile)
	}
	repository.mediaVerificationCache[cacheKey] = verifiedMediaFile{info: current, digest: media.SHA256}
	repository.mediaVerificationMu.Unlock()
	return nil
}
