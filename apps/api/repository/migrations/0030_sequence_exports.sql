CREATE TABLE sequence_export_artifacts (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  producer_job_id TEXT NOT NULL UNIQUE REFERENCES work_jobs(id) ON DELETE RESTRICT,
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
  output_profile TEXT NOT NULL CHECK (output_profile = 'webm-vp9-opus-v1'),
  state TEXT NOT NULL CHECK (state IN ('valid', 'invalid')),
  facts_json TEXT NOT NULL CHECK (json_valid(facts_json)),
  byte_reference TEXT NOT NULL CHECK (
    length(CAST(byte_reference AS BLOB)) BETWEEN 1 AND 256
  ),
  byte_size INTEGER NOT NULL CHECK (byte_size > 0),
  content_digest TEXT NOT NULL CHECK (
    content_digest GLOB 'sha256:*' AND length(content_digest) = 71
  ),
  created_at TEXT NOT NULL
) STRICT;

CREATE INDEX sequence_export_artifacts_sequence
ON sequence_export_artifacts(project_id, sequence_id, sequence_revision, state, id);

CREATE TABLE sequence_export_job_details (
  job_id TEXT PRIMARY KEY NOT NULL REFERENCES work_jobs(id) ON DELETE RESTRICT,
  sequence_id TEXT NOT NULL REFERENCES sequences(id) ON DELETE RESTRICT,
  sequence_revision INTEGER NOT NULL CHECK (sequence_revision >= 1),
  preset TEXT NOT NULL CHECK (preset = 'webm-vp9-opus-v1'),
  resolver_version TEXT NOT NULL CHECK (
    resolver_version = 'sequence-export-input-resolver-v1'
  ),
  compiler_version TEXT NOT NULL CHECK (compiler_version = 'sequence-render-plan-v4'),
  renderer_version TEXT NOT NULL CHECK (
    length(CAST(renderer_version AS BLOB)) BETWEEN 1 AND 1024
  ),
  renderer_target TEXT NOT NULL CHECK (
    length(CAST(renderer_target AS BLOB)) BETWEEN 1 AND 128
  ),
  render_intent_schema TEXT NOT NULL CHECK (
    render_intent_schema = 'open-cut/sequence-render-intent/v1'
  ),
  render_intent_digest TEXT NOT NULL CHECK (
    render_intent_digest GLOB 'sha256:*' AND length(render_intent_digest) = 71
  ),
  render_intent_json TEXT NOT NULL CHECK (json_valid(render_intent_json)),
  render_plan_digest TEXT REFERENCES render_plans(digest) ON DELETE RESTRICT,
  result_artifact_id TEXT REFERENCES sequence_export_artifacts(id) ON DELETE RESTRICT,
  CHECK (
    (render_plan_digest IS NULL AND result_artifact_id IS NULL) OR
    render_plan_digest IS NOT NULL
  )
) STRICT;

CREATE INDEX sequence_export_job_details_sequence
ON sequence_export_job_details(sequence_id, sequence_revision, job_id);

CREATE UNIQUE INDEX sequence_export_jobs_one_retry
ON work_jobs(retry_of_job_id)
WHERE kind = 'sequence-export' AND retry_of_job_id IS NOT NULL;

CREATE TABLE sequence_export_job_inputs (
  job_id TEXT NOT NULL REFERENCES sequence_export_job_details(job_id) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
  clip_id TEXT NOT NULL REFERENCES clips(id) ON DELETE RESTRICT,
  source_stream_id TEXT NOT NULL REFERENCES source_streams(id) ON DELETE RESTRICT,
  producer_job_id TEXT NOT NULL REFERENCES media_job_details(job_id) ON DELETE RESTRICT,
  PRIMARY KEY (job_id, ordinal),
  UNIQUE (job_id, clip_id)
) STRICT;

CREATE INDEX sequence_export_job_inputs_producer
ON sequence_export_job_inputs(producer_job_id, job_id);

CREATE TABLE sequence_export_job_resources (
  job_id TEXT NOT NULL REFERENCES sequence_export_job_details(job_id) ON DELETE RESTRICT,
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

CREATE TABLE sequence_export_requests (
  actor_id TEXT NOT NULL CHECK (length(CAST(actor_id AS BLOB)) BETWEEN 1 AND 128),
  request_id TEXT NOT NULL CHECK (length(CAST(request_id AS BLOB)) BETWEEN 1 AND 128),
  command TEXT NOT NULL CHECK (command IN ('start', 'cancel')),
  input_digest TEXT NOT NULL CHECK (
    input_digest GLOB 'sha256:*' AND length(input_digest) = 71
  ),
  input_json TEXT NOT NULL CHECK (json_valid(input_json)),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE RESTRICT,
  turn_id TEXT NOT NULL REFERENCES agent_turns(id) ON DELETE RESTRICT,
  job_id TEXT NOT NULL REFERENCES work_jobs(id) ON DELETE RESTRICT,
  activity_event_id TEXT NOT NULL REFERENCES activity_outbox(event_id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  PRIMARY KEY (actor_id, request_id)
) STRICT;

CREATE INDEX sequence_export_requests_job
ON sequence_export_requests(job_id, command, created_at);
