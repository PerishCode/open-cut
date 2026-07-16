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
