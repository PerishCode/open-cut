PRAGMA defer_foreign_keys = ON;

-- RenderPlan v4 removes Viewer-proxy vocabulary from the shared semantic plan.
-- Preview and sequence-frame work is fully derived, so discard that graph and
-- rebuild it against the one v4 decoder instead of retaining runtime legacy.
DELETE FROM sequence_frame_scratch_leases;

DELETE FROM work_job_prerequisites
WHERE job_id IN (SELECT id FROM work_jobs WHERE kind = 'sequence-frame-set');
DELETE FROM work_job_attempts
WHERE job_id IN (SELECT id FROM work_jobs WHERE kind = 'sequence-frame-set');
DELETE FROM work_job_owners
WHERE job_id IN (SELECT id FROM work_jobs WHERE kind = 'sequence-frame-set');
DELETE FROM sequence_frame_set_job_details;
DELETE FROM sequence_frame_set_artifacts;
DELETE FROM work_jobs WHERE kind = 'sequence-frame-set';

DROP INDEX sequence_frame_scratch_leases_set;
DROP INDEX sequence_frame_scratch_leases_turn;
DROP INDEX sequence_frame_scratch_leases_expiry;
DROP TABLE sequence_frame_scratch_leases;
DROP INDEX sequence_frame_set_job_details_preview;
DROP TABLE sequence_frame_set_job_details;
DROP INDEX sequence_frame_set_artifacts_sequence;
DROP TABLE sequence_frame_set_artifacts;

DELETE FROM work_job_prerequisites
WHERE job_id IN (SELECT id FROM work_jobs WHERE kind = 'sequence-preview');
DELETE FROM work_job_attempts
WHERE job_id IN (SELECT id FROM work_jobs WHERE kind = 'sequence-preview');
DELETE FROM work_job_owners
WHERE job_id IN (SELECT id FROM work_jobs WHERE kind = 'sequence-preview');
DELETE FROM sequence_preview_job_resources;
DELETE FROM sequence_preview_job_inputs;
DELETE FROM sequence_preview_job_details;
DELETE FROM sequence_preview_artifacts;
DELETE FROM work_jobs WHERE kind = 'sequence-preview';

DROP TABLE sequence_preview_job_resources;
DROP INDEX sequence_preview_job_inputs_producer;
DROP TABLE sequence_preview_job_inputs;
DROP INDEX sequence_preview_job_details_sequence;
DROP TABLE sequence_preview_job_details;
DROP INDEX sequence_preview_artifacts_project_sequence;
DROP TABLE sequence_preview_artifacts;

DELETE FROM render_plan_inputs;
DROP INDEX render_plan_inputs_artifact;
DROP TABLE render_plan_inputs;
DELETE FROM render_plans;
DROP INDEX render_plans_sequence_revision;
DROP TABLE render_plans;

-- Asset-scoped normalized render material belongs to the same artifact root as
-- facts, proxies, frames, and transcripts. The migration runner disables FK
-- enforcement around this transaction and verifies the complete graph before
-- commit so the referenced root can be rebuilt without a shadow artifact model.
CREATE TABLE media_artifacts_v2 (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
  kind TEXT NOT NULL CHECK (
    kind IN ('media-facts', 'frame-sample-set', 'proxy', 'render-input', 'waveform', 'transcript')
  ),
  producer_version TEXT NOT NULL,
  input_fingerprint TEXT NOT NULL CHECK (
    input_fingerprint GLOB 'sha256:*' AND length(input_fingerprint) = 71
  ),
  parameters_digest TEXT NOT NULL CHECK (
    parameters_digest GLOB 'sha256:*' AND length(parameters_digest) = 71
  ),
  parameters_json TEXT NOT NULL CHECK (json_valid(parameters_json)),
  state TEXT NOT NULL CHECK (state IN ('ready', 'evicted')),
  byte_reference TEXT NOT NULL,
  byte_size INTEGER NOT NULL CHECK (byte_size >= 0),
  content_digest TEXT NOT NULL CHECK (
    content_digest GLOB 'sha256:*' AND length(content_digest) = 71
  ),
  created_at TEXT NOT NULL,
  UNIQUE (asset_id, kind, producer_version, input_fingerprint, parameters_digest)
) STRICT;

INSERT INTO media_artifacts_v2 (
  id, project_id, asset_id, kind, producer_version, input_fingerprint,
  parameters_digest, parameters_json, state, byte_reference, byte_size,
  content_digest, created_at
)
SELECT id, project_id, asset_id, kind, producer_version, input_fingerprint,
       parameters_digest, parameters_json, state, byte_reference, byte_size,
       content_digest, created_at
FROM media_artifacts;

DROP TABLE media_artifacts;
ALTER TABLE media_artifacts_v2 RENAME TO media_artifacts;

CREATE TABLE render_plans (
  digest TEXT PRIMARY KEY NOT NULL CHECK (digest GLOB 'sha256:*' AND length(digest) = 71),
  schema_version TEXT NOT NULL CHECK (schema_version = 'open-cut/render-plan/v4'),
  compiler_version TEXT NOT NULL CHECK (compiler_version = 'sequence-render-plan-v4'),
  purpose TEXT NOT NULL CHECK (purpose IN ('sequence-preview', 'export')),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  sequence_id TEXT NOT NULL REFERENCES sequences(id) ON DELETE RESTRICT,
  sequence_revision INTEGER NOT NULL CHECK (sequence_revision >= 1),
  observed_project_revision INTEGER NOT NULL CHECK (observed_project_revision >= 1),
  output_profile TEXT NOT NULL CHECK (
    length(CAST(output_profile AS BLOB)) BETWEEN 1 AND 128
  ),
  canonical_json TEXT NOT NULL CHECK (json_valid(canonical_json)),
  created_at TEXT NOT NULL
) STRICT;

CREATE INDEX render_plans_sequence_revision
ON render_plans(sequence_id, sequence_revision, purpose, compiler_version, created_at, digest);

CREATE TABLE render_plan_inputs (
  plan_digest TEXT NOT NULL REFERENCES render_plans(digest) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
  artifact_id TEXT NOT NULL REFERENCES media_artifacts(id) ON DELETE RESTRICT,
  artifact_digest TEXT NOT NULL CHECK (
    artifact_digest GLOB 'sha256:*' AND length(artifact_digest) = 71
  ),
  PRIMARY KEY (plan_digest, ordinal),
  UNIQUE (plan_digest, artifact_id)
) STRICT;

CREATE INDEX render_plan_inputs_artifact
ON render_plan_inputs(artifact_id, plan_digest);

-- A render attempt reads immutable material for longer than one transaction.
-- Its work-attempt lease, not an Agent Turn, is the durable liveness authority.
CREATE TABLE render_material_leases (
  attempt_id TEXT NOT NULL REFERENCES work_job_attempts(id) ON DELETE RESTRICT,
  artifact_id TEXT NOT NULL REFERENCES media_artifacts(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  PRIMARY KEY (attempt_id, artifact_id)
) STRICT;

CREATE INDEX render_material_leases_artifact
ON render_material_leases(artifact_id, attempt_id);

CREATE TABLE sequence_preview_artifacts (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  sequence_id TEXT NOT NULL REFERENCES sequences(id) ON DELETE RESTRICT,
  sequence_revision INTEGER NOT NULL CHECK (sequence_revision >= 1),
  render_plan_digest TEXT NOT NULL REFERENCES render_plans(digest) ON DELETE RESTRICT,
  renderer_version TEXT NOT NULL CHECK (
    length(CAST(renderer_version AS BLOB)) BETWEEN 1 AND 1024
  ),
  renderer_target TEXT NOT NULL CHECK (
    length(CAST(renderer_target AS BLOB)) BETWEEN 1 AND 128
  ),
  output_profile TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('ready', 'evicted')),
  facts_json TEXT NOT NULL CHECK (json_valid(facts_json)),
  byte_reference TEXT NOT NULL,
  byte_size INTEGER NOT NULL CHECK (byte_size > 0),
  content_digest TEXT NOT NULL CHECK (
    content_digest GLOB 'sha256:*' AND length(content_digest) = 71
  ),
  created_at TEXT NOT NULL,
  UNIQUE (render_plan_digest, renderer_version, renderer_target, output_profile)
) STRICT;

CREATE INDEX sequence_preview_artifacts_project_sequence
ON sequence_preview_artifacts(project_id, sequence_id, sequence_revision, state, id);

CREATE TABLE sequence_preview_job_details (
  job_id TEXT PRIMARY KEY NOT NULL REFERENCES work_jobs(id) ON DELETE RESTRICT,
  sequence_id TEXT NOT NULL REFERENCES sequences(id) ON DELETE RESTRICT,
  sequence_revision INTEGER NOT NULL CHECK (sequence_revision >= 1),
  resolver_version TEXT NOT NULL,
  compiler_version TEXT NOT NULL,
  renderer_version TEXT NOT NULL,
  renderer_target TEXT NOT NULL,
  output_profile TEXT NOT NULL,
  render_intent_schema TEXT NOT NULL CHECK (
    render_intent_schema = 'open-cut/sequence-render-intent/v1'
  ),
  render_intent_digest TEXT NOT NULL CHECK (
    render_intent_digest GLOB 'sha256:*' AND length(render_intent_digest) = 71
  ),
  render_intent_json TEXT NOT NULL CHECK (json_valid(render_intent_json)),
  render_plan_digest TEXT REFERENCES render_plans(digest) ON DELETE RESTRICT,
  result_artifact_id TEXT REFERENCES sequence_preview_artifacts(id) ON DELETE RESTRICT,
  CHECK (
    (render_plan_digest IS NULL AND result_artifact_id IS NULL) OR
    render_plan_digest IS NOT NULL
  )
) STRICT;

CREATE INDEX sequence_preview_job_details_sequence
ON sequence_preview_job_details(sequence_id, sequence_revision, job_id);

CREATE TABLE sequence_preview_job_inputs (
  job_id TEXT NOT NULL REFERENCES sequence_preview_job_details(job_id) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
  clip_id TEXT NOT NULL REFERENCES clips(id) ON DELETE RESTRICT,
  source_stream_id TEXT NOT NULL REFERENCES source_streams(id) ON DELETE RESTRICT,
  producer_job_id TEXT NOT NULL REFERENCES media_job_details(job_id) ON DELETE RESTRICT,
  PRIMARY KEY (job_id, ordinal),
  UNIQUE (job_id, clip_id)
) STRICT;

CREATE INDEX sequence_preview_job_inputs_producer
ON sequence_preview_job_inputs(producer_job_id, job_id);

CREATE TABLE sequence_preview_job_resources (
  job_id TEXT NOT NULL REFERENCES sequence_preview_job_details(job_id) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
  resource_kind TEXT NOT NULL CHECK (
    length(CAST(resource_kind AS BLOB)) BETWEEN 1 AND 64
  ),
  resource_id TEXT NOT NULL CHECK (length(CAST(resource_id AS BLOB)) BETWEEN 1 AND 256),
  resource_version TEXT NOT NULL CHECK (
    length(CAST(resource_version AS BLOB)) BETWEEN 1 AND 128
  ),
  resource_digest TEXT NOT NULL CHECK (
    resource_digest GLOB 'sha256:*' AND length(resource_digest) = 71
  ),
  PRIMARY KEY (job_id, ordinal),
  UNIQUE (job_id, resource_kind, resource_id)
) STRICT;

CREATE TABLE sequence_frame_set_artifacts (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  producer_job_id TEXT NOT NULL UNIQUE REFERENCES work_jobs(id) ON DELETE RESTRICT,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  sequence_id TEXT NOT NULL REFERENCES sequences(id) ON DELETE RESTRICT,
  sequence_revision INTEGER NOT NULL CHECK (sequence_revision >= 1),
  preview_job_id TEXT NOT NULL REFERENCES sequence_preview_job_details(job_id) ON DELETE RESTRICT,
  preview_artifact_id TEXT NOT NULL REFERENCES sequence_preview_artifacts(id) ON DELETE RESTRICT,
  preview_artifact_digest TEXT NOT NULL CHECK (
    preview_artifact_digest GLOB 'sha256:*' AND length(preview_artifact_digest) = 71
  ),
  render_plan_digest TEXT NOT NULL REFERENCES render_plans(digest) ON DELETE RESTRICT,
  parameters_digest TEXT NOT NULL CHECK (
    parameters_digest GLOB 'sha256:*' AND length(parameters_digest) = 71
  ),
  producer_version TEXT NOT NULL CHECK (
    length(CAST(producer_version AS BLOB)) BETWEEN 1 AND 1024
  ),
  profile TEXT NOT NULL CHECK (profile = 'sequence-frame-srgb-png-v1'),
  grid_policy TEXT NOT NULL CHECK (grid_policy = 'sequence-frame-grid-floor-v1'),
  state TEXT NOT NULL CHECK (state IN ('ready', 'evicted')),
  byte_reference TEXT NOT NULL,
  byte_size INTEGER NOT NULL CHECK (byte_size > 0),
  content_digest TEXT NOT NULL CHECK (
    content_digest GLOB 'sha256:*' AND length(content_digest) = 71
  ),
  created_at TEXT NOT NULL
) STRICT;

CREATE INDEX sequence_frame_set_artifacts_sequence
ON sequence_frame_set_artifacts(project_id, sequence_id, sequence_revision, state, id);

CREATE TABLE sequence_frame_set_job_details (
  job_id TEXT PRIMARY KEY NOT NULL REFERENCES work_jobs(id) ON DELETE RESTRICT,
  sequence_id TEXT NOT NULL REFERENCES sequences(id) ON DELETE RESTRICT,
  sequence_revision INTEGER NOT NULL CHECK (sequence_revision >= 1),
  preview_job_id TEXT NOT NULL REFERENCES sequence_preview_job_details(job_id) ON DELETE RESTRICT,
  frame_rate_value INTEGER NOT NULL CHECK (frame_rate_value > 0),
  frame_rate_scale INTEGER NOT NULL CHECK (frame_rate_scale BETWEEN 1 AND 2147483647),
  grid_policy TEXT NOT NULL CHECK (grid_policy = 'sequence-frame-grid-floor-v1'),
  profile TEXT NOT NULL CHECK (profile = 'sequence-frame-srgb-png-v1'),
  preview_artifact_id TEXT REFERENCES sequence_preview_artifacts(id) ON DELETE RESTRICT,
  preview_artifact_digest TEXT CHECK (
    preview_artifact_digest IS NULL OR
    (preview_artifact_digest GLOB 'sha256:*' AND length(preview_artifact_digest) = 71)
  ),
  render_plan_digest TEXT REFERENCES render_plans(digest) ON DELETE RESTRICT,
  result_artifact_id TEXT REFERENCES sequence_frame_set_artifacts(id) ON DELETE RESTRICT,
  CHECK (
    (preview_artifact_id IS NULL AND preview_artifact_digest IS NULL AND render_plan_digest IS NULL AND result_artifact_id IS NULL) OR
    (preview_artifact_id IS NOT NULL AND preview_artifact_digest IS NOT NULL AND render_plan_digest IS NOT NULL)
  )
) STRICT;

CREATE INDEX sequence_frame_set_job_details_preview
ON sequence_frame_set_job_details(preview_job_id, job_id);

CREATE TABLE sequence_frame_scratch_leases (
  resource_id TEXT PRIMARY KEY NOT NULL CHECK (length(resource_id) = 36),
  lease_set_id TEXT NOT NULL CHECK (length(lease_set_id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  sequence_id TEXT NOT NULL REFERENCES sequences(id) ON DELETE RESTRICT,
  sequence_revision INTEGER NOT NULL CHECK (sequence_revision >= 1),
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE RESTRICT,
  turn_id TEXT NOT NULL REFERENCES agent_turns(id) ON DELETE RESTRICT,
  job_id TEXT NOT NULL REFERENCES work_jobs(id) ON DELETE RESTRICT,
  artifact_id TEXT NOT NULL REFERENCES sequence_frame_set_artifacts(id) ON DELETE RESTRICT,
  sample_index INTEGER NOT NULL CHECK (sample_index BETWEEN 0 AND 7),
  frame_index INTEGER NOT NULL CHECK (frame_index >= 0),
  relative_path TEXT NOT NULL CHECK (length(relative_path) BETWEEN 1 AND 512),
  mime_type TEXT NOT NULL CHECK (mime_type = 'image/png'),
  byte_size INTEGER NOT NULL CHECK (byte_size BETWEEN 1 AND 33554432),
  sha256 TEXT NOT NULL CHECK (sha256 GLOB 'sha256:*' AND length(sha256) = 71),
  requested_time_json TEXT NOT NULL CHECK (json_valid(requested_time_json)),
  sequence_time_json TEXT NOT NULL CHECK (json_valid(sequence_time_json)),
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (run_id, turn_id, relative_path),
  UNIQUE (turn_id, artifact_id, sample_index)
) STRICT;

CREATE INDEX sequence_frame_scratch_leases_expiry
ON sequence_frame_scratch_leases(expires_at, resource_id);
CREATE INDEX sequence_frame_scratch_leases_turn
ON sequence_frame_scratch_leases(turn_id, resource_id);
CREATE INDEX sequence_frame_scratch_leases_set
ON sequence_frame_scratch_leases(lease_set_id, sample_index);
