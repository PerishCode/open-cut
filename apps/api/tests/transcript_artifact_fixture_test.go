package tests

import (
	"database/sql"
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func cloneTranscriptArtifactFixture(
	t *testing.T,
	databasePath string,
	sourceArtifactID domain.ArtifactID,
) domain.ArtifactID {
	t.Helper()
	database, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	artifactID, _ := domain.ParseArtifactID("018f0a60-7b80-7a01-8000-000000000021")
	segmentID := "018f0a60-7b80-7a01-8000-000000000022"
	tokenIDs := []string{
		"018f0a60-7b80-7a01-8000-000000000023",
		"018f0a60-7b80-7a01-8000-000000000024",
	}
	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
	INSERT INTO media_artifacts (
	  id, project_id, asset_id, kind, producer_version, input_fingerprint,
	  parameters_digest, parameters_json, state, byte_reference, byte_size, content_digest, created_at
	)
	SELECT ?, project_id, asset_id, kind, producer_version || '-alternate', input_fingerprint,
	       parameters_digest, parameters_json, state, ?, byte_size, content_digest, ?
	FROM media_artifacts WHERE id = ?`,
		artifactID.String(), "sqlite:transcript/"+artifactID.String(),
		"2026-07-15T10:00:00.000000000Z", sourceArtifactID.String(),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`
	INSERT INTO transcript_artifacts (
	  artifact_id, schema_version, binding_digest, source_stream_id, model_resource_id,
	  detected_language, language_confidence_basis_points, source_start_value, source_start_scale,
	  sample_rate, channels, sample_format, sample_count, pcm_byte_size, pcm_digest,
	  channel_policy, timing_policy, segment_count, token_count
	)
	SELECT ?, schema_version, binding_digest, source_stream_id, model_resource_id,
	       detected_language, language_confidence_basis_points, source_start_value, source_start_scale,
	       sample_rate, channels, sample_format, sample_count, pcm_byte_size, pcm_digest,
	       channel_policy, timing_policy, segment_count, token_count
	FROM transcript_artifacts WHERE artifact_id = ?`, artifactID.String(), sourceArtifactID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`
	INSERT INTO transcript_segments (
	  id, artifact_id, ordinal, source_start_value, source_start_scale,
	  source_duration_value, source_duration_scale, text
	)
	SELECT ?, ?, ordinal, source_start_value, source_start_scale,
	       source_duration_value, source_duration_scale, text
	FROM transcript_segments WHERE artifact_id = ?`, segmentID, artifactID.String(), sourceArtifactID.String()); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(`
	SELECT token.ordinal, token.source_start_value, token.source_start_scale,
	       token.source_duration_value, token.source_duration_scale, token.text, token.confidence_basis_points
	FROM transcript_tokens token
	JOIN transcript_segments segment ON segment.id = token.segment_id
	WHERE segment.artifact_id = ? ORDER BY token.ordinal`, sourceArtifactID.String())
	if err != nil {
		t.Fatal(err)
	}
	index := 0
	for rows.Next() {
		var ordinal, startValue, startScale, durationValue, durationScale int64
		var text string
		var confidence sql.NullInt64
		if err := rows.Scan(&ordinal, &startValue, &startScale, &durationValue, &durationScale, &text, &confidence); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if index >= len(tokenIDs) {
			rows.Close()
			t.Fatal("unexpected transcript token count")
		}
		if _, err := tx.Exec(`
	INSERT INTO transcript_tokens (
	  id, segment_id, ordinal, source_start_value, source_start_scale,
	  source_duration_value, source_duration_scale, text, confidence_basis_points
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, tokenIDs[index], segmentID, ordinal, startValue, startScale,
			durationValue, durationScale, text, confidence); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		index++
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if index != len(tokenIDs) {
		t.Fatalf("cloned %d transcript tokens", index)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return artifactID
}
