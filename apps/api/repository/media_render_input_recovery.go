package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type storedRenderInputArtifact = storedFrameArtifact

func (repository *SQLiteProjects) loadStoredRenderInputArtifacts(
	ctx context.Context,
) (map[string]storedRenderInputArtifact, error) {
	rows, err := repository.db.QueryContext(ctx, `
SELECT id, asset_id, producer_version, input_fingerprint, parameters_digest,
       parameters_json, byte_reference, byte_size, content_digest
FROM media_artifacts
WHERE kind = 'render-input' AND state = 'ready'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make(map[string]storedRenderInputArtifact)
	for rows.Next() {
		var idValue, assetValue string
		var record storedRenderInputArtifact
		if err := rows.Scan(
			&idValue, &assetValue, &record.producer, &record.inputFingerprint,
			&record.parametersDigest, &record.parametersJSON, &record.byteReference,
			&record.byteSize, &record.contentDigest,
		); err != nil {
			return nil, err
		}
		var parseErr error
		record.id, parseErr = domain.ParseArtifactID(idValue)
		if parseErr != nil {
			return nil, parseErr
		}
		record.assetID, parseErr = domain.ParseAssetID(assetValue)
		if parseErr != nil {
			return nil, parseErr
		}
		records[idValue] = record
	}
	return records, rows.Err()
}

func (repository *SQLiteProjects) verifyStoredRenderInputArtifact(record storedRenderInputArtifact) error {
	if record.byteReference != "artifact:media/"+record.id.String() {
		return fmt.Errorf("unexpected byte reference")
	}
	root := filepath.Join(repository.dataDir, "artifacts", "media", record.id.String())
	if err := requireDirectory(root); err != nil {
		return err
	}
	manifestBytes, err := readBoundedRegularFile(
		filepath.Join(root, "manifest.json"), application.MaximumRenderInputManifestSize,
	)
	if err != nil {
		return err
	}
	manifestHash := sha256.Sum256(manifestBytes)
	if record.contentDigest != "sha256:"+hex.EncodeToString(manifestHash[:]) {
		return fmt.Errorf("manifest digest mismatch")
	}
	manifest, err := application.DecodeRenderInputArtifactManifest(manifestBytes)
	if err != nil {
		return err
	}
	parameters, err := application.DecodeInitialMediaJobParameters(record.parametersJSON)
	if err != nil {
		return err
	}
	canonicalParameters, digest, err := application.CanonicalInitialMediaJobParameters(parameters)
	if err != nil || !bytes.Equal(canonicalParameters, record.parametersJSON) ||
		digest.String() != record.parametersDigest {
		return fmt.Errorf("render-input parameters are not canonical")
	}
	if manifest.AssetID != record.assetID || manifest.AssetID != parameters.AssetID ||
		manifest.Fingerprint.String() != record.inputFingerprint ||
		manifest.Profile != parameters.Profile || parameters.Kind != domain.MediaJobRenderInput ||
		manifest.Producer != record.producer || !renderInputSelectionMatches(parameters, manifest) {
		return fmt.Errorf("manifest authority mismatch")
	}
	expectedEntries := []string{"manifest.json", "render-input.mkv"}
	if manifest.Video != nil {
		expectedEntries = append(expectedEntries, "video-time-map.bin")
	}
	if err := requireExactDirectoryEntries(root, expectedEntries); err != nil {
		return err
	}
	if err := verifyStoredRenderInputFileStructure(root, manifest.Media); err != nil {
		return err
	}
	total := uint64(len(manifestBytes)) + manifest.Media.ByteSize.Value()
	if manifest.Video != nil {
		if err := verifyStoredRenderInputFileDigest(root, manifest.Video.TimeMap); err != nil {
			return err
		}
		file, err := os.Open(filepath.Join(root, manifest.Video.TimeMap.Path))
		if err != nil {
			return err
		}
		validationErr := application.ValidateSourceProxyTimeMapReader(file, manifest.Video.FrameCount.Value())
		closeErr := file.Close()
		if validationErr != nil {
			return validationErr
		}
		if closeErr != nil {
			return closeErr
		}
		total += manifest.Video.TimeMap.ByteSize.Value()
	}
	if total != record.byteSize {
		return fmt.Errorf("artifact aggregate size mismatch")
	}
	return nil
}

func renderInputSelectionMatches(
	parameters application.InitialMediaJobParameters,
	manifest application.RenderInputArtifactManifest,
) bool {
	selection := parameters.RenderInputSelection
	if selection == nil {
		return false
	}
	if manifest.Video != nil {
		return selection.VideoStreamID != nil && selection.AudioStreamID == nil &&
			*selection.VideoStreamID == manifest.Video.Source.ID
	}
	return manifest.Audio != nil && selection.VideoStreamID == nil && selection.AudioStreamID != nil &&
		*selection.AudioStreamID == manifest.Audio.Source.ID
}

func verifyStoredRenderInputFileStructure(root string, record application.RenderInputArtifactFile) error {
	path := filepath.Join(root, filepath.FromSlash(record.Path))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		uint64(info.Size()) != record.ByteSize.Value() {
		return fmt.Errorf("artifact file is invalid")
	}
	return nil
}

func verifyStoredRenderInputFileDigest(root string, record application.RenderInputArtifactFile) error {
	if err := verifyStoredRenderInputFileStructure(root, record); err != nil {
		return err
	}
	file, err := os.Open(filepath.Join(root, filepath.FromSlash(record.Path)))
	if err != nil {
		return err
	}
	defer file.Close()
	digest := sha256.New()
	written, err := io.Copy(digest, file)
	if err != nil || uint64(written) != record.ByteSize.Value() ||
		"sha256:"+hex.EncodeToString(digest.Sum(nil)) != record.SHA256.String() {
		return fmt.Errorf("artifact file digest mismatch")
	}
	return nil
}
