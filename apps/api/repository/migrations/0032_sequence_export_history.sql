CREATE TABLE sequence_export_artifacts_v2 (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  producer_job_id TEXT NOT NULL UNIQUE REFERENCES work_jobs(id) ON DELETE RESTRICT,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  sequence_id TEXT NOT NULL REFERENCES sequences(id) ON DELETE RESTRICT,
  sequence_revision INTEGER NOT NULL CHECK (sequence_revision >= 1),
  render_plan_digest TEXT NOT NULL REFERENCES render_plans(digest) ON DELETE RESTRICT,
  renderer_version TEXT NOT NULL CHECK (length(CAST(renderer_version AS BLOB)) BETWEEN 1 AND 1024),
  renderer_target TEXT NOT NULL CHECK (length(CAST(renderer_target AS BLOB)) BETWEEN 1 AND 128),
  output_profile TEXT NOT NULL CHECK (output_profile = 'webm-vp9-opus-v1'),
  state TEXT NOT NULL CHECK (state IN ('valid', 'invalid', 'deleted')),
  facts_json TEXT NOT NULL CHECK (json_valid(facts_json)),
  byte_reference TEXT NOT NULL CHECK (length(CAST(byte_reference AS BLOB)) BETWEEN 1 AND 256),
  byte_size INTEGER NOT NULL CHECK (byte_size > 0),
  content_digest TEXT NOT NULL CHECK (
    content_digest GLOB 'sha256:*' AND length(content_digest) = 71
  ),
  created_at TEXT NOT NULL
) STRICT;

INSERT INTO sequence_export_artifacts_v2 (
  id, producer_job_id, project_id, sequence_id, sequence_revision,
  render_plan_digest, renderer_version, renderer_target, output_profile, state,
  facts_json, byte_reference, byte_size, content_digest, created_at
)
SELECT id, producer_job_id, project_id, sequence_id, sequence_revision,
       render_plan_digest, renderer_version, renderer_target, output_profile, state,
       facts_json, byte_reference, byte_size, content_digest, created_at
FROM sequence_export_artifacts;

DROP TABLE sequence_export_artifacts;
ALTER TABLE sequence_export_artifacts_v2 RENAME TO sequence_export_artifacts;

CREATE INDEX sequence_export_artifacts_sequence
ON sequence_export_artifacts(project_id, sequence_id, sequence_revision, state, id);

CREATE TABLE sequence_export_requests_v2 (
  actor_id TEXT NOT NULL CHECK (length(CAST(actor_id AS BLOB)) BETWEEN 1 AND 128),
  request_id TEXT NOT NULL CHECK (length(CAST(request_id AS BLOB)) BETWEEN 1 AND 128),
  command TEXT NOT NULL CHECK (command IN ('start', 'cancel', 'delete-artifact')),
  input_digest TEXT NOT NULL CHECK (
    input_digest GLOB 'sha256:*' AND length(input_digest) = 71
  ),
  input_json TEXT NOT NULL CHECK (json_valid(input_json)),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  owner_kind TEXT NOT NULL CHECK (owner_kind IN ('run', 'creator')),
  owner_id TEXT NOT NULL CHECK (length(CAST(owner_id AS BLOB)) BETWEEN 1 AND 128),
  run_id TEXT REFERENCES agent_runs(id) ON DELETE RESTRICT,
  turn_id TEXT REFERENCES agent_turns(id) ON DELETE RESTRICT,
  job_id TEXT NOT NULL REFERENCES work_jobs(id) ON DELETE RESTRICT,
  activity_event_id TEXT NOT NULL REFERENCES activity_outbox(event_id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  CHECK (
    (owner_kind = 'run' AND owner_id = run_id AND run_id IS NOT NULL AND turn_id IS NOT NULL) OR
    (owner_kind = 'creator' AND run_id IS NULL AND turn_id IS NULL)
  ),
  PRIMARY KEY (actor_id, request_id)
) STRICT;

INSERT INTO sequence_export_requests_v2 (
  actor_id, request_id, command, input_digest, input_json, project_id,
  owner_kind, owner_id, run_id, turn_id, job_id, activity_event_id, created_at
)
SELECT actor_id, request_id, command, input_digest, input_json, project_id,
       owner_kind, owner_id, run_id, turn_id, job_id, activity_event_id, created_at
FROM sequence_export_requests;

DROP TABLE sequence_export_requests;
ALTER TABLE sequence_export_requests_v2 RENAME TO sequence_export_requests;

CREATE INDEX sequence_export_requests_job
ON sequence_export_requests(job_id, command, created_at);

CREATE INDEX sequence_export_history_roots
ON work_jobs(project_id, created_at DESC, id DESC)
WHERE kind = 'sequence-export' AND retry_of_job_id IS NULL;
