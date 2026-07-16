CREATE TABLE work_jobs (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  scope_kind TEXT NOT NULL CHECK (scope_kind IN ('project', 'installation')),
  project_id TEXT REFERENCES projects(id) ON DELETE RESTRICT,
  installation_id TEXT CHECK (
    installation_id IS NULL OR length(CAST(installation_id AS BLOB)) BETWEEN 1 AND 128
  ),
  kind TEXT NOT NULL CHECK (length(CAST(kind AS BLOB)) BETWEEN 1 AND 128),
  state TEXT NOT NULL CHECK (
    state IN ('blocked', 'queued', 'running', 'succeeded', 'failed', 'cancelled')
  ),
  pool TEXT NOT NULL CHECK (pool IN ('interactive-cpu', 'io', 'cpu', 'gpu', 'network')),
  priority_class TEXT NOT NULL CHECK (
    priority_class IN ('interactive', 'foreground', 'background')
  ),
  logical_key TEXT NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 1024),
  parameters_digest TEXT NOT NULL CHECK (
    parameters_digest GLOB 'sha256:*' AND length(parameters_digest) = 71
  ),
  parameters_json TEXT NOT NULL CHECK (json_valid(parameters_json)),
  producer_version TEXT NOT NULL CHECK (
    length(CAST(producer_version AS BLOB)) BETWEEN 1 AND 1024
  ),
  progress_basis_points INTEGER NOT NULL DEFAULT 0 CHECK (
    progress_basis_points BETWEEN 0 AND 10000
  ),
  cancellation_requested INTEGER NOT NULL DEFAULT 0 CHECK (
    cancellation_requested IN (0, 1)
  ),
  retry_of_job_id TEXT REFERENCES work_jobs(id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  terminal_error_code TEXT CHECK (
    terminal_error_code IS NULL OR length(CAST(terminal_error_code AS BLOB)) BETWEEN 1 AND 256
  ),
  CHECK (
    (scope_kind = 'project' AND project_id IS NOT NULL AND installation_id IS NULL) OR
    (scope_kind = 'installation' AND project_id IS NULL AND installation_id IS NOT NULL)
  )
) STRICT;

CREATE UNIQUE INDEX work_jobs_live_logical_key
ON work_jobs(logical_key)
WHERE state IN ('blocked', 'queued', 'running');

CREATE INDEX work_jobs_claim
ON work_jobs(state, priority_class, created_at, id);

CREATE INDEX work_jobs_project_state
ON work_jobs(project_id, state, kind, id)
WHERE project_id IS NOT NULL;

CREATE TABLE media_job_details (
  job_id TEXT PRIMARY KEY NOT NULL REFERENCES work_jobs(id) ON DELETE RESTRICT,
  asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
  result_artifact_id TEXT REFERENCES media_artifacts(id) ON DELETE RESTRICT
) STRICT;

CREATE INDEX media_job_details_asset
ON media_job_details(asset_id, job_id);

CREATE TABLE work_job_owners (
  job_id TEXT NOT NULL REFERENCES work_jobs(id) ON DELETE RESTRICT,
  owner_kind TEXT NOT NULL CHECK (length(CAST(owner_kind AS BLOB)) BETWEEN 1 AND 64),
  owner_id TEXT NOT NULL CHECK (length(CAST(owner_id AS BLOB)) BETWEEN 1 AND 128),
  created_at TEXT NOT NULL,
  PRIMARY KEY (job_id, owner_kind, owner_id)
) STRICT;

CREATE TABLE work_job_attempts (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  job_id TEXT NOT NULL REFERENCES work_jobs(id) ON DELETE RESTRICT,
  generation INTEGER NOT NULL CHECK (generation >= 1),
  state TEXT NOT NULL CHECK (
    state IN ('leased', 'running', 'publishing', 'succeeded', 'failed', 'abandoned')
  ),
  lease_owner TEXT NOT NULL,
  lease_expires_at TEXT NOT NULL,
  heartbeat_at TEXT NOT NULL,
  started_at TEXT NOT NULL,
  ended_at TEXT,
  executor_version TEXT NOT NULL,
  temporary_output_identity TEXT,
  diagnostics_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(diagnostics_json)),
  UNIQUE (job_id, generation)
) STRICT;

CREATE UNIQUE INDEX work_job_attempts_one_live
ON work_job_attempts(job_id)
WHERE state IN ('leased', 'running', 'publishing');

CREATE TABLE work_job_prerequisites (
  job_id TEXT NOT NULL REFERENCES work_jobs(id) ON DELETE RESTRICT,
  kind TEXT NOT NULL CHECK (length(CAST(kind AS BLOB)) BETWEEN 1 AND 128),
  reference_kind TEXT NOT NULL CHECK (
    reference_kind IN ('job', 'resource', 'capability', 'artifact')
  ),
  reference_id TEXT NOT NULL CHECK (length(CAST(reference_id AS BLOB)) BETWEEN 1 AND 256),
  created_at TEXT NOT NULL,
  PRIMARY KEY (job_id, kind, reference_kind, reference_id)
) STRICT;

CREATE INDEX work_job_prerequisites_reference
ON work_job_prerequisites(reference_kind, reference_id, job_id);

INSERT INTO work_jobs (
  id, scope_kind, project_id, installation_id, kind, state, pool, priority_class,
  logical_key, parameters_digest, parameters_json, producer_version,
  progress_basis_points, cancellation_requested, retry_of_job_id, created_at,
  updated_at, terminal_error_code
)
SELECT id, 'project', project_id, NULL, kind, state, pool, priority_class,
       logical_key, parameters_digest, parameters_json, producer_version,
       progress_basis_points, cancellation_requested, retry_of_job_id, created_at,
       updated_at, terminal_error_code
FROM media_jobs;

INSERT INTO media_job_details (job_id, asset_id, result_artifact_id)
SELECT id, asset_id, result_artifact_id FROM media_jobs;

INSERT INTO work_job_owners (job_id, owner_kind, owner_id, created_at)
SELECT job_id, owner_kind, owner_id, created_at FROM media_job_owners;

INSERT INTO work_job_attempts (
  id, job_id, generation, state, lease_owner, lease_expires_at, heartbeat_at,
  started_at, ended_at, executor_version, temporary_output_identity, diagnostics_json
)
SELECT id, job_id, generation, state, lease_owner, lease_expires_at, heartbeat_at,
       started_at, ended_at, executor_version, temporary_output_identity, diagnostics_json
FROM media_job_attempts;

INSERT INTO work_job_prerequisites (
  job_id, kind, reference_kind, reference_id, created_at
)
SELECT job_id, kind, reference_kind, reference_id, created_at
FROM media_job_prerequisites;

DROP TABLE media_job_prerequisites;
DROP TABLE media_job_attempts;
DROP TABLE media_job_owners;
DROP TABLE media_jobs;
