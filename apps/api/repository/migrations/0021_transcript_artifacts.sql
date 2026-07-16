ALTER TABLE media_job_details
ADD COLUMN result_code TEXT CHECK (result_code IS NULL OR result_code IN ('no-audio'));

CREATE TABLE transcript_job_bindings (
  job_id TEXT PRIMARY KEY NOT NULL REFERENCES media_job_details(job_id) ON DELETE RESTRICT,
  schema_version TEXT NOT NULL CHECK (schema_version = 'open-cut/transcript-binding/v1'),
  binding_digest TEXT NOT NULL CHECK (
    binding_digest GLOB 'sha256:*' AND length(binding_digest) = 71
  ),
  source_stream_id TEXT NOT NULL REFERENCES source_streams(id) ON DELETE RESTRICT,
  source_descriptor_digest TEXT NOT NULL CHECK (
    source_descriptor_digest GLOB 'sha256:*' AND length(source_descriptor_digest) = 71
  ),
  selection_policy TEXT NOT NULL CHECK (selection_policy = 'default-audio-v1'),
  normalization_policy TEXT NOT NULL CHECK (
    normalization_policy = 'pcm-s16-mono-16000-source-time-v1'
  ),
  language_policy TEXT NOT NULL CHECK (language_policy = 'auto-original-v1'),
  engine_version TEXT NOT NULL CHECK (
    length(CAST(engine_version AS BLOB)) BETWEEN 1 AND 1024
  ),
  engine_target TEXT NOT NULL CHECK (
    length(CAST(engine_target AS BLOB)) BETWEEN 1 AND 128
  ),
  model_resource_id TEXT NOT NULL REFERENCES product_resources(id) ON DELETE RESTRICT,
  model_name TEXT NOT NULL CHECK (
    length(CAST(model_name AS BLOB)) BETWEEN 1 AND 128
  ),
  model_version TEXT NOT NULL CHECK (
    length(CAST(model_version AS BLOB)) BETWEEN 1 AND 128
  ),
  model_entry_digest TEXT NOT NULL CHECK (
    model_entry_digest GLOB 'sha256:*' AND length(model_entry_digest) = 71
  ),
  model_content_digest TEXT NOT NULL CHECK (
    model_content_digest GLOB 'sha256:*' AND length(model_content_digest) = 71
  ),
  canonical_json TEXT NOT NULL CHECK (json_valid(canonical_json)),
  created_at TEXT NOT NULL
) STRICT;

CREATE INDEX transcript_job_bindings_resource
ON transcript_job_bindings(model_resource_id, job_id);

CREATE TABLE transcript_artifacts (
  artifact_id TEXT PRIMARY KEY NOT NULL REFERENCES media_artifacts(id) ON DELETE RESTRICT,
  schema_version TEXT NOT NULL CHECK (schema_version = 'open-cut/transcript-artifact/v1'),
  binding_digest TEXT NOT NULL CHECK (
    binding_digest GLOB 'sha256:*' AND length(binding_digest) = 71
  ),
  source_stream_id TEXT NOT NULL REFERENCES source_streams(id) ON DELETE RESTRICT,
  model_resource_id TEXT NOT NULL REFERENCES product_resources(id) ON DELETE RESTRICT,
  detected_language TEXT NOT NULL CHECK (
    length(CAST(detected_language AS BLOB)) BETWEEN 1 AND 64
  ),
  language_confidence_basis_points INTEGER CHECK (
    language_confidence_basis_points IS NULL OR
    language_confidence_basis_points BETWEEN 0 AND 10000
  ),
  source_start_value INTEGER NOT NULL,
  source_start_scale INTEGER NOT NULL CHECK (source_start_scale BETWEEN 1 AND 2147483647),
  sample_rate INTEGER NOT NULL CHECK (sample_rate = 16000),
  channels INTEGER NOT NULL CHECK (channels = 1),
  sample_format TEXT NOT NULL CHECK (sample_format = 's16le'),
  sample_count INTEGER NOT NULL CHECK (sample_count BETWEEN 1 AND 8000000000),
  pcm_byte_size INTEGER NOT NULL CHECK (pcm_byte_size = sample_count * 2),
  pcm_digest TEXT NOT NULL CHECK (
    pcm_digest GLOB 'sha256:*' AND length(pcm_digest) = 71
  ),
  channel_policy TEXT NOT NULL CHECK (
    channel_policy IN ('mono-pass-v1', 'stereo-equal-v1')
  ),
  timing_policy TEXT NOT NULL CHECK (timing_policy = 'audio-frame-pts-gap-fill-v1'),
  segment_count INTEGER NOT NULL CHECK (segment_count BETWEEN 0 AND 100000),
  token_count INTEGER NOT NULL CHECK (token_count BETWEEN 0 AND 1000000),
  UNIQUE (binding_digest, artifact_id)
) STRICT;

CREATE INDEX transcript_artifacts_source
ON transcript_artifacts(source_stream_id, artifact_id);

CREATE TABLE transcript_segments (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  artifact_id TEXT NOT NULL REFERENCES transcript_artifacts(artifact_id) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 99999),
  source_start_value INTEGER NOT NULL,
  source_start_scale INTEGER NOT NULL CHECK (source_start_scale BETWEEN 1 AND 2147483647),
  source_duration_value INTEGER NOT NULL CHECK (source_duration_value > 0),
  source_duration_scale INTEGER NOT NULL CHECK (source_duration_scale BETWEEN 1 AND 2147483647),
  text TEXT NOT NULL CHECK (length(CAST(text AS BLOB)) BETWEEN 1 AND 8192),
  UNIQUE (artifact_id, ordinal)
) STRICT;

CREATE INDEX transcript_segments_page
ON transcript_segments(artifact_id, ordinal, id);

CREATE TABLE transcript_tokens (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  segment_id TEXT NOT NULL REFERENCES transcript_segments(id) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 65535),
  source_start_value INTEGER NOT NULL,
  source_start_scale INTEGER NOT NULL CHECK (source_start_scale BETWEEN 1 AND 2147483647),
  source_duration_value INTEGER NOT NULL CHECK (source_duration_value > 0),
  source_duration_scale INTEGER NOT NULL CHECK (source_duration_scale BETWEEN 1 AND 2147483647),
  text TEXT NOT NULL CHECK (length(CAST(text AS BLOB)) BETWEEN 1 AND 512),
  confidence_basis_points INTEGER CHECK (
    confidence_basis_points IS NULL OR confidence_basis_points BETWEEN 0 AND 10000
  ),
  UNIQUE (segment_id, ordinal)
) STRICT;

CREATE INDEX transcript_tokens_segment
ON transcript_tokens(segment_id, ordinal, id);

CREATE TABLE asset_transcript_selection (
  asset_id TEXT PRIMARY KEY NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
  artifact_id TEXT NOT NULL UNIQUE REFERENCES transcript_artifacts(artifact_id) ON DELETE RESTRICT,
  selected_at TEXT NOT NULL
) STRICT;
