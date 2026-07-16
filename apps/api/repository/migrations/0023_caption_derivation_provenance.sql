PRAGMA defer_foreign_keys = ON;

-- Every Caption has one explicit origin. Existing creative captions are manual;
-- transcript derivation adds typed immutable evidence without changing the
-- independently editable Caption projection.
CREATE TABLE caption_provenance (
  caption_id TEXT PRIMARY KEY NOT NULL REFERENCES captions(id) ON DELETE RESTRICT,
  kind TEXT NOT NULL CHECK (kind IN ('manual', 'transcript-derivation'))
) STRICT;

INSERT INTO caption_provenance (caption_id, kind)
SELECT id, 'manual' FROM captions;

CREATE TABLE caption_derivations (
  caption_id TEXT PRIMARY KEY NOT NULL REFERENCES caption_provenance(caption_id) ON DELETE RESTRICT,
  source_excerpt_id TEXT NOT NULL REFERENCES narrative_source_excerpt_values(id) ON DELETE RESTRICT,
  source_excerpt_revision INTEGER NOT NULL CHECK (source_excerpt_revision >= 1),
  asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
  accepted_fingerprint TEXT NOT NULL CHECK (
    accepted_fingerprint GLOB 'sha256:*' AND length(accepted_fingerprint) = 71
  ),
  transcript_artifact_id TEXT NOT NULL REFERENCES transcript_artifacts(artifact_id) ON DELETE RESTRICT,
  source_stream_id TEXT NOT NULL REFERENCES source_streams(id) ON DELETE RESTRICT,
  clip_id TEXT NOT NULL REFERENCES clips(id) ON DELETE RESTRICT,
  clip_revision INTEGER NOT NULL CHECK (clip_revision >= 1),
  clip_source_start_value INTEGER NOT NULL,
  clip_source_start_scale INTEGER NOT NULL CHECK (clip_source_start_scale BETWEEN 1 AND 2147483647),
  clip_source_duration_value INTEGER NOT NULL CHECK (clip_source_duration_value > 0),
  clip_source_duration_scale INTEGER NOT NULL CHECK (clip_source_duration_scale BETWEEN 1 AND 2147483647),
  clip_timeline_start_value INTEGER NOT NULL,
  clip_timeline_start_scale INTEGER NOT NULL CHECK (clip_timeline_start_scale BETWEEN 1 AND 2147483647),
  clip_timeline_duration_value INTEGER NOT NULL CHECK (clip_timeline_duration_value > 0),
  clip_timeline_duration_scale INTEGER NOT NULL CHECK (clip_timeline_duration_scale BETWEEN 1 AND 2147483647),
  evidence_start_value INTEGER NOT NULL,
  evidence_start_scale INTEGER NOT NULL CHECK (evidence_start_scale BETWEEN 1 AND 2147483647),
  evidence_duration_value INTEGER NOT NULL CHECK (evidence_duration_value > 0),
  evidence_duration_scale INTEGER NOT NULL CHECK (evidence_duration_scale BETWEEN 1 AND 2147483647),
  policy_id TEXT NOT NULL CHECK (policy_id = 'readable-captions-v1'),
  policy_maximum_lines INTEGER NOT NULL CHECK (policy_maximum_lines = 2),
  policy_maximum_line_graphemes INTEGER NOT NULL CHECK (policy_maximum_line_graphemes = 42),
  policy_minimum_duration_value INTEGER NOT NULL CHECK (policy_minimum_duration_value = 1),
  policy_minimum_duration_scale INTEGER NOT NULL CHECK (policy_minimum_duration_scale = 1),
  policy_maximum_duration_value INTEGER NOT NULL CHECK (policy_maximum_duration_value = 6),
  policy_maximum_duration_scale INTEGER NOT NULL CHECK (policy_maximum_duration_scale = 1),
  policy_maximum_gap_value INTEGER NOT NULL CHECK (policy_maximum_gap_value = 3),
  policy_maximum_gap_scale INTEGER NOT NULL CHECK (policy_maximum_gap_scale = 4),
  policy_maximum_reading_rate INTEGER NOT NULL CHECK (policy_maximum_reading_rate = 20),
  policy_boundary TEXT NOT NULL CHECK (policy_boundary = 'terminal-punctuation-v1'),
  policy_timing TEXT NOT NULL CHECK (policy_timing = 'forward-pad-no-overlap-v1'),
  policy_unicode TEXT NOT NULL CHECK (policy_unicode = 'unicode-egc-15.0.0-uniseg-v0.4.7'),
  derived_start_value INTEGER NOT NULL,
  derived_start_scale INTEGER NOT NULL CHECK (derived_start_scale BETWEEN 1 AND 2147483647),
  derived_duration_value INTEGER NOT NULL CHECK (derived_duration_value > 0),
  derived_duration_scale INTEGER NOT NULL CHECK (derived_duration_scale BETWEEN 1 AND 2147483647),
  derived_language TEXT NOT NULL CHECK (
    length(CAST(derived_language AS BLOB)) BETWEEN 2 AND 64 AND
    derived_language NOT GLOB '*[^A-Za-z0-9-]*'
  ),
  derived_text TEXT NOT NULL CHECK (length(CAST(derived_text AS BLOB)) BETWEEN 1 AND 262144)
) STRICT;

CREATE INDEX caption_derivations_source
ON caption_derivations(source_excerpt_id, clip_id, caption_id);

CREATE TABLE caption_derivation_segments (
  caption_id TEXT NOT NULL REFERENCES caption_derivations(caption_id) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 255),
  segment_id TEXT NOT NULL REFERENCES transcript_segments(id) ON DELETE RESTRICT,
  PRIMARY KEY (caption_id, ordinal),
  UNIQUE (caption_id, segment_id)
) STRICT;

CREATE TABLE caption_derivation_corrections (
  caption_id TEXT NOT NULL REFERENCES caption_derivations(caption_id) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 255),
  correction_id TEXT NOT NULL REFERENCES transcript_corrections(id) ON DELETE RESTRICT,
  correction_revision INTEGER NOT NULL CHECK (correction_revision >= 1),
  PRIMARY KEY (caption_id, ordinal),
  UNIQUE (caption_id, correction_id)
) STRICT;
