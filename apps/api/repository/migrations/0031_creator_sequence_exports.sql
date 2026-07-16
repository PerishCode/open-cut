ALTER TABLE sequence_export_requests RENAME TO sequence_export_requests_agent_only;

CREATE TABLE sequence_export_requests (
  actor_id TEXT NOT NULL CHECK (length(CAST(actor_id AS BLOB)) BETWEEN 1 AND 128),
  request_id TEXT NOT NULL CHECK (length(CAST(request_id AS BLOB)) BETWEEN 1 AND 128),
  command TEXT NOT NULL CHECK (command IN ('start', 'cancel')),
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

INSERT INTO sequence_export_requests (
  actor_id, request_id, command, input_digest, input_json, project_id,
  owner_kind, owner_id, run_id, turn_id, job_id, activity_event_id, created_at
)
SELECT actor_id, request_id, command, input_digest, input_json, project_id,
       'run', run_id, run_id, turn_id, job_id, activity_event_id, created_at
FROM sequence_export_requests_agent_only;

DROP INDEX sequence_export_requests_job;
DROP TABLE sequence_export_requests_agent_only;

CREATE INDEX sequence_export_requests_job
ON sequence_export_requests(job_id, command, created_at);
