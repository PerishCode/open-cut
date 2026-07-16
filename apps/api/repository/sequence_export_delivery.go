package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/PerishCode/open-cut/product/application"
	"github.com/PerishCode/open-cut/product/domain"
)

func (repository *SQLiteProjects) InspectSequenceExportDelivery(
	ctx context.Context,
	projectID domain.ProjectID,
	artifactID domain.ArtifactID,
	at time.Time,
) (application.SequenceExportArtifactFile, error) {
	if projectID.IsZero() || artifactID.IsZero() || at.IsZero() {
		return application.SequenceExportArtifactFile{}, application.ErrSequenceExportInvalid
	}
	repository.artifactLifecycleMu.Lock()
	defer repository.artifactLifecycleMu.Unlock()
	record, err := repository.loadSequenceExportDeliveryRecord(ctx, artifactID)
	if err != nil {
		return application.SequenceExportArtifactFile{}, err
	}
	if record.projectID != projectID || record.state != domain.SequenceExportArtifactValid {
		return application.SequenceExportArtifactFile{}, application.ErrSequenceExportNotFound
	}
	manifest, err := repository.inspectStoredSequenceExportArtifact(record)
	if err != nil {
		return application.SequenceExportArtifactFile{}, repository.rejectSequenceExportDelivery(ctx, record, at, err)
	}
	return manifest.Media, nil
}

func (repository *SQLiteProjects) OpenSequenceExportDelivery(
	ctx context.Context,
	projectID domain.ProjectID,
	artifactID domain.ArtifactID,
	at time.Time,
) (*os.File, application.SequenceExportArtifactFile, error) {
	if projectID.IsZero() || artifactID.IsZero() || at.IsZero() {
		return nil, application.SequenceExportArtifactFile{}, application.ErrSequenceExportInvalid
	}
	repository.artifactLifecycleMu.Lock()
	defer repository.artifactLifecycleMu.Unlock()
	record, err := repository.loadSequenceExportDeliveryRecord(ctx, artifactID)
	if err != nil {
		return nil, application.SequenceExportArtifactFile{}, err
	}
	if record.projectID != projectID || record.state != domain.SequenceExportArtifactValid {
		return nil, application.SequenceExportArtifactFile{}, application.ErrSequenceExportNotFound
	}
	file, manifest, err := repository.openStoredSequenceExportArtifact(record)
	if err != nil {
		return nil, application.SequenceExportArtifactFile{}, repository.rejectSequenceExportDelivery(ctx, record, at, err)
	}
	return file, manifest.Media, nil
}

func (repository *SQLiteProjects) loadSequenceExportDeliveryRecord(
	ctx context.Context,
	artifactID domain.ArtifactID,
) (storedSequenceExportArtifact, error) {
	var (
		idValue, producerValue, projectValue, sequenceValue, planValue string
		stateValue, factsJSON, contentDigestValue                      string
		revisionValue                                                  uint64
		record                                                         storedSequenceExportArtifact
	)
	err := repository.db.QueryRowContext(ctx, `
SELECT id, producer_job_id, project_id, sequence_id, sequence_revision,
       render_plan_digest, renderer_version, renderer_target, output_profile,
       state, facts_json, byte_reference, byte_size, content_digest
FROM sequence_export_artifacts WHERE id = ?`, artifactID.String()).Scan(
		&idValue, &producerValue, &projectValue, &sequenceValue, &revisionValue,
		&planValue, &record.rendererVersion, &record.rendererTarget, &record.profile,
		&stateValue, &factsJSON, &record.byteReference, &record.byteSize, &contentDigestValue,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return storedSequenceExportArtifact{}, application.ErrSequenceExportNotFound
	}
	if err != nil {
		return storedSequenceExportArtifact{}, err
	}
	record.id, err = domain.ParseArtifactID(idValue)
	if err == nil {
		record.producerJobID, err = domain.ParseWorkJobID(producerValue)
	}
	if err == nil {
		record.projectID, err = domain.ParseProjectID(projectValue)
	}
	if err == nil {
		record.sequenceID, err = domain.ParseSequenceID(sequenceValue)
	}
	if err == nil {
		record.sequenceRevision, err = domain.NewRevision(revisionValue)
	}
	if err == nil {
		record.planDigest, err = domain.ParseDigest(planValue)
	}
	if err == nil {
		record.contentDigest, err = domain.ParseDigest(contentDigestValue)
	}
	record.state = domain.SequenceExportArtifactState(stateValue)
	if err != nil || json.Unmarshal([]byte(factsJSON), &record.facts) != nil ||
		application.ValidateSequenceExportFacts(record.facts) != nil ||
		record.profile != domain.SequenceExportProfileV1 ||
		(record.state != domain.SequenceExportArtifactValid && record.state != domain.SequenceExportArtifactInvalid &&
			record.state != domain.SequenceExportArtifactDeleted) {
		return storedSequenceExportArtifact{}, application.ErrSequenceExportInvalid
	}
	return record, nil
}

func (repository *SQLiteProjects) rejectSequenceExportDelivery(
	ctx context.Context,
	record storedSequenceExportArtifact,
	at time.Time,
	cause error,
) error {
	if err := repository.invalidateStoredSequenceExportArtifact(ctx, record, at.UTC()); err != nil {
		return err
	}
	canonicalRoot := filepath.Join(repository.dataDir, "artifacts", "sequence-export", record.id.String())
	quarantineRoot := filepath.Join(repository.dataDir, "work", "sequence-export-quarantine")
	if err := os.MkdirAll(quarantineRoot, 0o700); err != nil {
		return err
	}
	if err := quarantineSequenceExportRoot(canonicalRoot, quarantineRoot, record.id); err != nil {
		return err
	}
	return fmt.Errorf("%w: %v", application.ErrSequenceExportIntegrity, cause)
}
