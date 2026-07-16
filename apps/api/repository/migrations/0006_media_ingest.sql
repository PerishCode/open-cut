CREATE TABLE source_grants (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  installation_id TEXT NOT NULL CHECK (length(installation_id) BETWEEN 1 AND 128),
  platform TEXT NOT NULL CHECK (platform IN ('mac', 'win', 'linux')),
  grant_kind TEXT NOT NULL CHECK (grant_kind IN ('local-path-v1', 'mac-security-scoped-bookmark-v1')),
  schema_version TEXT NOT NULL,
  protected_material BLOB NOT NULL CHECK (length(protected_material) BETWEEN 1 AND 65536),
  display_name TEXT NOT NULL CHECK (length(CAST(display_name AS BLOB)) BETWEEN 1 AND 2048),
  observed_byte_size INTEGER NOT NULL CHECK (observed_byte_size >= 0),
  observed_modified_unix_ns INTEGER NOT NULL,
  observed_file_identity TEXT NOT NULL CHECK (length(CAST(observed_file_identity AS BLOB)) BETWEEN 1 AND 2048),
  state TEXT NOT NULL CHECK (state IN ('active', 'revoked', 'unavailable')),
  created_at TEXT NOT NULL,
  last_resolved_at TEXT
) STRICT;

CREATE INDEX source_grants_installation_state
ON source_grants(installation_id, state, id);

CREATE TABLE source_grant_requests (
  creator_id TEXT NOT NULL REFERENCES local_creators(id) ON DELETE RESTRICT,
  request_id TEXT NOT NULL CHECK (length(request_id) BETWEEN 1 AND 128),
  input_digest TEXT NOT NULL CHECK (input_digest GLOB 'sha256:*' AND length(input_digest) = 71),
  source_grant_id TEXT NOT NULL REFERENCES source_grants(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  PRIMARY KEY (creator_id, request_id)
) STRICT;

CREATE TABLE assets (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision >= 1),
  source_grant_id TEXT NOT NULL REFERENCES source_grants(id) ON DELETE RESTRICT,
  display_name TEXT NOT NULL CHECK (length(CAST(display_name AS BLOB)) BETWEEN 1 AND 2048),
  import_mode TEXT NOT NULL CHECK (import_mode IN ('referenced', 'managed')),
  accepted_fingerprint TEXT CHECK (
    accepted_fingerprint IS NULL OR
    (accepted_fingerprint GLOB 'sha256:*' AND length(accepted_fingerprint) = 71)
  ),
  tombstoned INTEGER NOT NULL CHECK (tombstoned IN (0, 1)),
  last_transaction_id TEXT NOT NULL REFERENCES edit_transactions(id) DEFERRABLE INITIALLY DEFERRED,
  created_at TEXT NOT NULL,
  UNIQUE (project_id, source_grant_id)
) STRICT;

CREATE INDEX assets_project_page ON assets(project_id, tombstoned, id);

CREATE TABLE asset_media_state (
  asset_id TEXT PRIMARY KEY NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
  availability TEXT NOT NULL CHECK (
    availability IN ('identifying', 'online', 'changed', 'missing', 'managed', 'unreadable')
  ),
  observed_fingerprint TEXT CHECK (
    observed_fingerprint IS NULL OR
    (observed_fingerprint GLOB 'sha256:*' AND length(observed_fingerprint) = 71)
  ),
  facts_json TEXT CHECK (facts_json IS NULL OR json_valid(facts_json)),
  updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE source_streams (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
  fingerprint TEXT NOT NULL CHECK (fingerprint GLOB 'sha256:*' AND length(fingerprint) = 71),
  descriptor_digest TEXT NOT NULL CHECK (descriptor_digest GLOB 'sha256:*' AND length(descriptor_digest) = 71),
  media_type TEXT NOT NULL CHECK (media_type IN ('video', 'audio')),
  descriptor_json TEXT NOT NULL CHECK (json_valid(descriptor_json)),
  UNIQUE (asset_id, fingerprint, descriptor_digest)
) STRICT;

CREATE TABLE media_artifacts (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
  kind TEXT NOT NULL CHECK (kind IN ('media-facts', 'frame-sample-set', 'proxy', 'waveform', 'transcript')),
  producer_version TEXT NOT NULL,
  input_fingerprint TEXT NOT NULL CHECK (input_fingerprint GLOB 'sha256:*' AND length(input_fingerprint) = 71),
  parameters_digest TEXT NOT NULL CHECK (parameters_digest GLOB 'sha256:*' AND length(parameters_digest) = 71),
  parameters_json TEXT NOT NULL CHECK (json_valid(parameters_json)),
  state TEXT NOT NULL CHECK (state IN ('ready', 'evicted')),
  byte_reference TEXT NOT NULL,
  byte_size INTEGER NOT NULL CHECK (byte_size >= 0),
  content_digest TEXT NOT NULL CHECK (content_digest GLOB 'sha256:*' AND length(content_digest) = 71),
  created_at TEXT NOT NULL,
  UNIQUE (asset_id, kind, producer_version, input_fingerprint, parameters_digest)
) STRICT;

CREATE TABLE media_jobs (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
  kind TEXT NOT NULL CHECK (kind IN ('identify', 'probe', 'frame-sample-set', 'proxy', 'waveform', 'transcript')),
  state TEXT NOT NULL CHECK (state IN ('blocked', 'queued', 'running', 'succeeded', 'failed', 'cancelled')),
  pool TEXT NOT NULL CHECK (pool IN ('interactive-cpu', 'io', 'cpu', 'gpu', 'network')),
  priority_class TEXT NOT NULL CHECK (priority_class IN ('interactive', 'foreground', 'background')),
  logical_key TEXT NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 1024),
  parameters_digest TEXT NOT NULL CHECK (parameters_digest GLOB 'sha256:*' AND length(parameters_digest) = 71),
  parameters_json TEXT NOT NULL CHECK (json_valid(parameters_json)),
  producer_version TEXT NOT NULL,
  blocked_reason TEXT,
  progress_basis_points INTEGER NOT NULL DEFAULT 0 CHECK (progress_basis_points BETWEEN 0 AND 10000),
  cancellation_requested INTEGER NOT NULL DEFAULT 0 CHECK (cancellation_requested IN (0, 1)),
  result_artifact_id TEXT REFERENCES media_artifacts(id) ON DELETE RESTRICT,
  retry_of_job_id TEXT REFERENCES media_jobs(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  terminal_error_code TEXT
) STRICT;

CREATE UNIQUE INDEX media_jobs_live_logical_key
ON media_jobs(logical_key)
WHERE state IN ('blocked', 'queued', 'running', 'succeeded');

CREATE INDEX media_jobs_asset_state ON media_jobs(asset_id, state, kind, id);
CREATE INDEX media_jobs_claim ON media_jobs(state, priority_class, created_at, id);

CREATE TABLE media_job_owners (
  job_id TEXT NOT NULL REFERENCES media_jobs(id) ON DELETE RESTRICT,
  owner_kind TEXT NOT NULL CHECK (owner_kind IN ('project', 'asset', 'run')),
  owner_id TEXT NOT NULL CHECK (length(owner_id) BETWEEN 1 AND 128),
  created_at TEXT NOT NULL,
  PRIMARY KEY (job_id, owner_kind, owner_id)
) STRICT;

CREATE TABLE media_job_attempts (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  job_id TEXT NOT NULL REFERENCES media_jobs(id) ON DELETE RESTRICT,
  generation INTEGER NOT NULL CHECK (generation >= 1),
  state TEXT NOT NULL CHECK (state IN ('leased', 'running', 'publishing', 'succeeded', 'failed', 'abandoned')),
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

CREATE UNIQUE INDEX media_job_attempts_one_live
ON media_job_attempts(job_id)
WHERE state IN ('leased', 'running', 'publishing');
