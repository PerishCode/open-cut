PRAGMA defer_foreign_keys = ON;

-- Exact decoded proxy sample count is now canonical RenderPlan input. Preview
-- jobs, plans, and artifacts are derived state, so remove the v2 graph and
-- rebuild the closed v3 tables rather than retaining a dual decoder.
DELETE FROM work_job_prerequisites
WHERE job_id IN (SELECT id FROM work_jobs WHERE kind = 'sequence-preview');

DELETE FROM work_job_attempts
WHERE job_id IN (SELECT id FROM work_jobs WHERE kind = 'sequence-preview');

DELETE FROM work_job_owners
WHERE job_id IN (SELECT id FROM work_jobs WHERE kind = 'sequence-preview');

DELETE FROM sequence_preview_job_resources;
DELETE FROM sequence_preview_job_inputs;
DELETE FROM sequence_preview_job_details;
DELETE FROM work_jobs WHERE kind = 'sequence-preview';

DROP TABLE sequence_preview_job_resources;
DROP TABLE sequence_preview_job_inputs;
DROP INDEX sequence_preview_job_details_sequence;
DROP TABLE sequence_preview_job_details;

DELETE FROM sequence_preview_artifacts;
DROP INDEX sequence_preview_artifacts_project_sequence;
DROP TABLE sequence_preview_artifacts;

DELETE FROM render_plan_inputs;
DROP INDEX render_plan_inputs_artifact;
DROP TABLE render_plan_inputs;
DELETE FROM render_plans;
DROP INDEX render_plans_sequence_revision;
DROP TABLE render_plans;

CREATE TABLE render_plans (
  digest TEXT PRIMARY KEY NOT NULL CHECK (digest GLOB 'sha256:*' AND length(digest) = 71),
  schema_version TEXT NOT NULL CHECK (schema_version = 'open-cut/render-plan/v3'),
  compiler_version TEXT NOT NULL CHECK (compiler_version = 'sequence-render-plan-v3'),
  purpose TEXT NOT NULL CHECK (purpose = 'sequence-preview'),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  sequence_id TEXT NOT NULL REFERENCES sequences(id) ON DELETE RESTRICT,
  sequence_revision INTEGER NOT NULL CHECK (sequence_revision >= 1),
  observed_project_revision INTEGER NOT NULL CHECK (observed_project_revision >= 1),
  output_profile TEXT NOT NULL CHECK (output_profile = 'webm-vp9-opus-sequence-preview-v1'),
  canonical_json TEXT NOT NULL CHECK (json_valid(canonical_json)),
  created_at TEXT NOT NULL
) STRICT;

CREATE INDEX render_plans_sequence_revision
ON render_plans(sequence_id, sequence_revision, purpose, compiler_version, created_at, digest);

CREATE TABLE render_plan_inputs (
  plan_digest TEXT NOT NULL REFERENCES render_plans(digest) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
  artifact_id TEXT NOT NULL REFERENCES media_artifacts(id) ON DELETE RESTRICT,
  artifact_digest TEXT NOT NULL CHECK (artifact_digest GLOB 'sha256:*' AND length(artifact_digest) = 71),
  PRIMARY KEY (plan_digest, ordinal),
  UNIQUE (plan_digest, artifact_id)
) STRICT;

CREATE INDEX render_plan_inputs_artifact
ON render_plan_inputs(artifact_id, plan_digest);

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
    render_intent_schema = 'open-cut/sequence-preview-render-intent/v2'
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
