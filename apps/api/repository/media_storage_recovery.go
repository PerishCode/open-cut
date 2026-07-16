package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

type storedFrameArtifact struct {
	id               domain.ArtifactID
	assetID          domain.AssetID
	producer         string
	inputFingerprint string
	parametersDigest string
	parametersJSON   []byte
	byteReference    string
	byteSize         uint64
	contentDigest    string
}

type storedProxyArtifact = storedFrameArtifact

// ReconcileProductStorage restores durable artifact and ephemeral scratch
// invariants before the API advertises readiness after a process restart.
func (repository *SQLiteProjects) ReconcileProductStorage(ctx context.Context, now time.Time) error {
	if now.IsZero() {
		return application.ErrProductStorageInvalid
	}
	if err := repository.reconcileMediaPublicationWork(); err != nil {
		return err
	}
	if err := repository.reconcileMediaEvictionWork(ctx); err != nil {
		return err
	}
	if err := repository.reconcileMediaAttemptWork(); err != nil {
		return err
	}
	if err := repository.ReconcileProductResourceStorage(ctx); err != nil {
		return err
	}
	if err := repository.ReconcileMediaArtifactStorage(ctx); err != nil {
		return err
	}
	if err := repository.ReconcileSequencePreviewStorage(ctx); err != nil {
		return err
	}
	if err := repository.ReconcileSequenceExportStorage(ctx, now.UTC()); err != nil {
		return err
	}
	if err := repository.ReconcileSequenceFrameStorage(ctx, now.UTC()); err != nil {
		return err
	}
	return repository.ReconcileMediaScratchLeases(ctx, now.UTC())
}

func (repository *SQLiteProjects) reconcileMediaEvictionWork(ctx context.Context) error {
	repository.artifactLifecycleMu.Lock()
	defer repository.artifactLifecycleMu.Unlock()
	root := filepath.Join(repository.dataDir, "work", "media-evictions")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	artifactRoot := filepath.Join(repository.dataDir, "artifacts", "media")
	if err := os.MkdirAll(artifactRoot, 0o700); err != nil {
		return err
	}
	for _, eventEntry := range entries {
		if _, err := domain.ParseActivityEventID(eventEntry.Name()); err != nil || !eventEntry.IsDir() {
			continue
		}
		eventRoot := filepath.Join(root, eventEntry.Name())
		artifacts, readErr := os.ReadDir(eventRoot)
		if readErr != nil || len(artifacts) != 1 || !artifacts[0].IsDir() {
			continue
		}
		artifactID, parseErr := domain.ParseArtifactID(artifacts[0].Name())
		if parseErr != nil {
			continue
		}
		var state string
		err := repository.db.QueryRowContext(ctx, `
SELECT state FROM media_artifacts WHERE id = ? AND kind = 'proxy'`, artifactID.String()).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		quarantineRoot := filepath.Join(eventRoot, artifactID.String())
		canonicalRoot := filepath.Join(artifactRoot, artifactID.String())
		switch domain.ArtifactState(state) {
		case domain.ArtifactReady:
			if _, err := os.Lstat(canonicalRoot); !os.IsNotExist(err) {
				continue
			}
			if err := os.Rename(quarantineRoot, canonicalRoot); err != nil {
				return err
			}
			if err := os.Remove(eventRoot); err != nil {
				return err
			}
			if err := syncDirectory(artifactRoot); err != nil {
				return err
			}
		case domain.ArtifactEvicted:
			if err := os.RemoveAll(eventRoot); err != nil {
				return err
			}
		default:
			return application.ErrAssetInvalid
		}
	}
	return syncDirectory(root)
}

// ReconcileMediaArtifactStorage removes only structurally recognized orphan
// artifacts and rejects malformed durable metadata. Large proxy media bytes are
// verified before first delivery, outside API readiness. Unknown entries are
// deliberately left untouched.
func (repository *SQLiteProjects) ReconcileMediaArtifactStorage(ctx context.Context) error {
	artifactRoot := filepath.Join(repository.dataDir, "artifacts", "media")
	if err := os.MkdirAll(artifactRoot, 0o700); err != nil {
		return err
	}
	records, err := repository.loadStoredFrameArtifacts(ctx)
	if err != nil {
		return err
	}
	proxyRecords, err := repository.loadStoredProxyArtifacts(ctx)
	if err != nil {
		return err
	}
	renderInputRecords, err := repository.loadStoredRenderInputArtifacts(ctx)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(artifactRoot)
	if err != nil {
		return err
	}
	removed := false
	for _, entry := range entries {
		artifactID, parseErr := domain.ParseArtifactID(entry.Name())
		if parseErr != nil {
			continue
		}
		_, retainedFrame := records[artifactID.String()]
		_, retainedProxy := proxyRecords[artifactID.String()]
		_, retainedRenderInput := renderInputRecords[artifactID.String()]
		if retainedFrame || retainedProxy || retainedRenderInput {
			continue
		}
		if err := os.RemoveAll(filepath.Join(artifactRoot, entry.Name())); err != nil {
			return err
		}
		removed = true
	}
	if removed {
		if err := syncDirectory(artifactRoot); err != nil {
			return err
		}
	}
	for _, record := range records {
		if err := repository.verifyStoredFrameArtifact(record); err != nil {
			return fmt.Errorf("verify ready frame artifact %s: %w", record.id, err)
		}
	}
	for _, record := range proxyRecords {
		if err := repository.verifyStoredProxyArtifact(record); err != nil {
			return fmt.Errorf("verify ready proxy artifact %s: %w", record.id, err)
		}
	}
	for _, record := range renderInputRecords {
		if err := repository.verifyStoredRenderInputArtifact(record); err != nil {
			return fmt.Errorf("verify ready render-input artifact %s: %w", record.id, err)
		}
	}
	return nil
}

func (repository *SQLiteProjects) loadStoredFrameArtifacts(
	ctx context.Context,
) (map[string]storedFrameArtifact, error) {
	rows, err := repository.db.QueryContext(ctx, `
SELECT id, asset_id, producer_version, input_fingerprint, parameters_digest,
       parameters_json, byte_reference, byte_size, content_digest
FROM media_artifacts
WHERE kind = 'frame-sample-set' AND state = 'ready'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make(map[string]storedFrameArtifact)
	for rows.Next() {
		var idValue, assetValue string
		var record storedFrameArtifact
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

func (repository *SQLiteProjects) verifyStoredFrameArtifact(record storedFrameArtifact) error {
	expectedReference := "artifact:media/" + record.id.String()
	if record.byteReference != expectedReference {
		return fmt.Errorf("unexpected byte reference")
	}
	root := filepath.Join(repository.dataDir, "artifacts", "media", record.id.String())
	if err := requireDirectory(root); err != nil {
		return err
	}
	manifestPath := filepath.Join(root, "manifest.json")
	manifestBytes, err := readBoundedRegularFile(manifestPath, application.MaximumFrameSetArtifactSize)
	if err != nil {
		return err
	}
	manifestHash := sha256.Sum256(manifestBytes)
	if record.contentDigest != "sha256:"+hex.EncodeToString(manifestHash[:]) {
		return fmt.Errorf("manifest digest mismatch")
	}
	manifest, err := application.DecodeFrameSetArtifactManifest(manifestBytes)
	if err != nil {
		return err
	}
	parameters, err := application.DecodeFrameSetParameters(record.parametersJSON)
	if err != nil {
		return err
	}
	canonicalParameters, digest, err := application.CanonicalFrameSetParameters(parameters)
	if err != nil || !bytes.Equal(canonicalParameters, record.parametersJSON) ||
		digest.String() != record.parametersDigest {
		return fmt.Errorf("frame parameters are not canonical")
	}
	if manifest.AssetID != record.assetID || manifest.AssetID != parameters.AssetID ||
		manifest.Fingerprint.String() != record.inputFingerprint ||
		manifest.Fingerprint != parameters.Fingerprint ||
		manifest.SourceStreamID != parameters.SourceStreamID || manifest.Profile != parameters.Profile ||
		manifest.Producer != record.producer || len(manifest.Samples) != len(parameters.Times) {
		return fmt.Errorf("manifest authority mismatch")
	}
	if err := requireExactDirectoryEntries(root, []string{"frames", "manifest.json"}); err != nil {
		return err
	}
	framesRoot := filepath.Join(root, "frames")
	if err := requireDirectory(framesRoot); err != nil {
		return err
	}
	expectedFrames := make([]string, len(manifest.Samples))
	total := uint64(len(manifestBytes))
	for index, sample := range manifest.Samples {
		if sample.RequestedTime != parameters.Times[index] {
			return fmt.Errorf("sample request mismatch")
		}
		expectedFrames[index] = fmt.Sprintf("%03d.png", index)
		frameBytes, readErr := readBoundedRegularFile(
			filepath.Join(root, filepath.FromSlash(sample.Path)), application.MaximumFrameSetArtifactSize,
		)
		if readErr != nil {
			return readErr
		}
		frameHash := sha256.Sum256(frameBytes)
		if uint64(len(frameBytes)) != sample.ByteSize.Value() ||
			sample.SHA256.String() != "sha256:"+hex.EncodeToString(frameHash[:]) {
			return fmt.Errorf("sample digest mismatch")
		}
		configuration, decodeErr := png.DecodeConfig(bytes.NewReader(frameBytes))
		if decodeErr != nil || configuration.Width != int(sample.Width) || configuration.Height != int(sample.Height) {
			return fmt.Errorf("sample PNG mismatch")
		}
		total += uint64(len(frameBytes))
	}
	if total != record.byteSize {
		return fmt.Errorf("artifact aggregate size mismatch")
	}
	return requireExactDirectoryEntries(framesRoot, expectedFrames)
}

func (repository *SQLiteProjects) loadStoredProxyArtifacts(
	ctx context.Context,
) (map[string]storedProxyArtifact, error) {
	rows, err := repository.db.QueryContext(ctx, `
SELECT id, asset_id, producer_version, input_fingerprint, parameters_digest,
       parameters_json, byte_reference, byte_size, content_digest
FROM media_artifacts
WHERE kind = 'proxy' AND state = 'ready'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make(map[string]storedProxyArtifact)
	for rows.Next() {
		var idValue, assetValue string
		var record storedProxyArtifact
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

func (repository *SQLiteProjects) verifyStoredProxyArtifact(record storedProxyArtifact) error {
	if record.byteReference != "artifact:media/"+record.id.String() {
		return fmt.Errorf("unexpected byte reference")
	}
	root := filepath.Join(repository.dataDir, "artifacts", "media", record.id.String())
	if err := requireDirectory(root); err != nil {
		return err
	}
	manifestBytes, err := readBoundedRegularFile(
		filepath.Join(root, "manifest.json"), application.MaximumSourceProxyManifestSize,
	)
	if err != nil {
		return err
	}
	manifestHash := sha256.Sum256(manifestBytes)
	if record.contentDigest != "sha256:"+hex.EncodeToString(manifestHash[:]) {
		return fmt.Errorf("manifest digest mismatch")
	}
	manifest, err := application.DecodeSourceProxyArtifactManifest(manifestBytes)
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
		return fmt.Errorf("proxy parameters are not canonical")
	}
	if manifest.AssetID != record.assetID || manifest.AssetID != parameters.AssetID ||
		manifest.Fingerprint.String() != record.inputFingerprint ||
		manifest.Profile != parameters.Profile || parameters.Kind != domain.MediaJobProxy ||
		manifest.Producer != record.producer {
		return fmt.Errorf("manifest authority mismatch")
	}
	expectedEntries := []string{"manifest.json", "proxy.webm"}
	if manifest.Video != nil {
		expectedEntries = append(expectedEntries, "video-time-map.bin")
	}
	if err := requireExactDirectoryEntries(root, expectedEntries); err != nil {
		return err
	}
	if err := verifyStoredArtifactFileStructure(root, manifest.Media); err != nil {
		return err
	}
	total := uint64(len(manifestBytes)) + manifest.Media.ByteSize.Value()
	if manifest.Video != nil {
		if err := verifyStoredArtifactFileDigest(root, manifest.Video.TimeMap); err != nil {
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

func verifyStoredArtifactFileStructure(root string, record application.SourceProxyArtifactFile) error {
	path := filepath.Join(root, filepath.FromSlash(record.Path))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		uint64(info.Size()) != record.ByteSize.Value() {
		return fmt.Errorf("artifact file is invalid")
	}
	return nil
}

func verifyStoredArtifactFileDigest(root string, record application.SourceProxyArtifactFile) error {
	if err := verifyStoredArtifactFileStructure(root, record); err != nil {
		return err
	}
	path := filepath.Join(root, filepath.FromSlash(record.Path))
	file, err := os.Open(path)
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

func requireDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("artifact directory is invalid")
	}
	return nil
}

func requireExactDirectoryEntries(path string, expected []string) error {
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != len(expected) {
		return fmt.Errorf("artifact directory contents are invalid")
	}
	want := make(map[string]struct{}, len(expected))
	for _, name := range expected {
		want[name] = struct{}{}
	}
	for _, entry := range entries {
		if _, found := want[entry.Name()]; !found {
			return fmt.Errorf("artifact directory contents are invalid")
		}
	}
	return nil
}

func (repository *SQLiteProjects) reconcileMediaPublicationWork() error {
	root := filepath.Join(repository.dataDir, "work", "media-publication")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if len(name) != 73 || name[36] != '-' {
			continue
		}
		if _, err := domain.ParseJobAttemptID(name[:36]); err != nil {
			continue
		}
		if _, err := domain.ParseArtifactID(name[37:]); err != nil {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
			return err
		}
	}
	return nil
}

func (repository *SQLiteProjects) reconcileMediaAttemptWork() error {
	root := filepath.Join(repository.dataDir, "work", "media-attempts")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	removed := false
	for _, entry := range entries {
		if _, err := domain.ParseJobAttemptID(entry.Name()); err != nil {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
		removed = true
	}
	if removed {
		return syncDirectory(root)
	}
	return nil
}

type activeScratchLease struct {
	kind       string
	leaseSetID string
	resourceID string
	runID      string
	turnID     string
	relative   string
	batch      string
	byteSize   uint64
	digest     string
}

func (repository *SQLiteProjects) reconcileScratchStorage(ctx context.Context) error {
	if err := repository.reconcileScratchPublicationWork(); err != nil {
		return err
	}
	leases, err := repository.loadActiveScratchLeases(ctx)
	if err != nil {
		return err
	}
	invalidBatches := make(map[string]struct{})
	for _, lease := range leases {
		data, readErr := readBoundedRegularFile(
			filepath.Join(repository.dataDir, filepath.FromSlash(lease.relative)),
			application.MaximumFrameSetArtifactSize,
		)
		digest := sha256.Sum256(data)
		if readErr != nil || uint64(len(data)) != lease.byteSize ||
			lease.digest != "sha256:"+hex.EncodeToString(digest[:]) {
			invalidBatches[lease.batch] = struct{}{}
		}
	}
	if len(invalidBatches) > 0 {
		if err := repository.revokeScratchBatches(ctx, leases, invalidBatches); err != nil {
			return err
		}
	}
	activeBatches := make(map[string]struct{})
	activePaths := make(map[string]struct{})
	for _, lease := range leases {
		if _, invalid := invalidBatches[lease.batch]; invalid {
			continue
		}
		activeBatches[lease.batch] = struct{}{}
		activePaths[lease.relative] = struct{}{}
	}
	return repository.pruneScratchOrphans(activeBatches, activePaths)
}

func (repository *SQLiteProjects) loadActiveScratchLeases(
	ctx context.Context,
) ([]activeScratchLease, error) {
	rows, err := repository.db.QueryContext(ctx, `
SELECT 'media', '', resource_id, run_id, turn_id, relative_path, byte_size, sha256
FROM media_scratch_leases
UNION ALL
SELECT 'sequence-frame', lease_set_id, resource_id, run_id, turn_id, relative_path, byte_size, sha256
FROM sequence_frame_scratch_leases`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	leases := make([]activeScratchLease, 0)
	for rows.Next() {
		var lease activeScratchLease
		if err := rows.Scan(
			&lease.kind, &lease.leaseSetID, &lease.resourceID, &lease.runID, &lease.turnID,
			&lease.relative, &lease.byteSize, &lease.digest,
		); err != nil {
			return nil, err
		}
		batch, valid := parseScratchLeaseRelative(lease.relative, lease.runID, lease.turnID, lease.resourceID)
		if !valid {
			return nil, application.ErrAssetInvalid
		}
		if lease.kind == "sequence-frame" {
			if lease.leaseSetID == "" || filepath.Base(filepath.FromSlash(batch)) != lease.leaseSetID {
				return nil, application.ErrSequenceFramesInvalid
			}
		} else if lease.kind != "media" || lease.leaseSetID != "" {
			return nil, application.ErrAssetInvalid
		}
		lease.batch = batch
		leases = append(leases, lease)
	}
	return leases, rows.Err()
}

func parseScratchLeaseRelative(relative, runValue, turnValue, resourceValue string) (string, bool) {
	if !validScratchRelative(filepath.FromSlash(relative)) || filepath.ToSlash(filepath.Clean(relative)) != relative {
		return "", false
	}
	parts := strings.Split(relative, "/")
	if len(parts) != 7 || parts[0] != "scratch" || parts[1] != "runs" || parts[2] != runValue ||
		parts[3] != "turns" || parts[4] != turnValue || parts[6] != resourceValue+".png" {
		return "", false
	}
	if _, err := domain.ParseRunID(parts[2]); err != nil {
		return "", false
	}
	if _, err := domain.ParseTurnID(parts[4]); err != nil {
		return "", false
	}
	if _, err := domain.ParseResourceID(parts[5]); err != nil {
		return "", false
	}
	if _, err := domain.ParseResourceID(resourceValue); err != nil {
		return "", false
	}
	return strings.Join(parts[:6], "/"), true
}

func (repository *SQLiteProjects) revokeScratchBatches(
	ctx context.Context,
	leases []activeScratchLease,
	invalid map[string]struct{},
) error {
	tx, err := repository.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, lease := range leases {
		if _, found := invalid[lease.batch]; !found {
			continue
		}
		table := "media_scratch_leases"
		if lease.kind == "sequence-frame" {
			table = "sequence_frame_scratch_leases"
		} else if lease.kind != "media" {
			return application.ErrAssetInvalid
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE resource_id = ?`, lease.resourceID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for batch := range invalid {
		if err := os.RemoveAll(filepath.Join(repository.dataDir, filepath.FromSlash(batch))); err != nil {
			return err
		}
	}
	return nil
}

func (repository *SQLiteProjects) reconcileScratchPublicationWork() error {
	root := filepath.Join(repository.dataDir, "work", "scratch-publication")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err := domain.ParseResourceID(entry.Name()); err != nil {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (repository *SQLiteProjects) pruneScratchOrphans(
	activeBatches map[string]struct{},
	activePaths map[string]struct{},
) error {
	runsRoot := filepath.Join(repository.dataDir, "scratch", "runs")
	runs, err := os.ReadDir(runsRoot)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, run := range runs {
		if _, err := domain.ParseRunID(run.Name()); err != nil || !run.IsDir() {
			continue
		}
		turnsRoot := filepath.Join(runsRoot, run.Name(), "turns")
		turns, readErr := os.ReadDir(turnsRoot)
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			return readErr
		}
		for _, turn := range turns {
			if _, err := domain.ParseTurnID(turn.Name()); err != nil || !turn.IsDir() {
				continue
			}
			turnRoot := filepath.Join(turnsRoot, turn.Name())
			batches, readErr := os.ReadDir(turnRoot)
			if readErr != nil {
				return readErr
			}
			for _, batch := range batches {
				if _, err := domain.ParseResourceID(batch.Name()); err != nil {
					continue
				}
				batchRelative := filepath.ToSlash(filepath.Join(
					"scratch", "runs", run.Name(), "turns", turn.Name(), batch.Name(),
				))
				batchPath := filepath.Join(turnRoot, batch.Name())
				if _, active := activeBatches[batchRelative]; !active {
					if err := os.RemoveAll(batchPath); err != nil {
						return err
					}
					continue
				}
				files, readErr := os.ReadDir(batchPath)
				if readErr != nil {
					return readErr
				}
				for _, file := range files {
					name := strings.TrimSuffix(file.Name(), ".png")
					if name == file.Name() {
						continue
					}
					if _, err := domain.ParseResourceID(name); err != nil {
						continue
					}
					relative := batchRelative + "/" + file.Name()
					if _, active := activePaths[relative]; !active {
						if err := os.Remove(filepath.Join(batchPath, file.Name())); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}
