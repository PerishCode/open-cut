CREATE TABLE media_scratch_leases (
  resource_id TEXT PRIMARY KEY NOT NULL CHECK (length(resource_id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE RESTRICT,
  turn_id TEXT NOT NULL REFERENCES agent_turns(id) ON DELETE RESTRICT,
  artifact_id TEXT NOT NULL REFERENCES media_artifacts(id) ON DELETE RESTRICT,
  sample_index INTEGER NOT NULL CHECK (sample_index BETWEEN 0 AND 7),
  relative_path TEXT NOT NULL CHECK (length(relative_path) BETWEEN 1 AND 512),
  mime_type TEXT NOT NULL CHECK (mime_type = 'image/png'),
  byte_size INTEGER NOT NULL CHECK (byte_size BETWEEN 1 AND 33554432),
  sha256 TEXT NOT NULL CHECK (sha256 GLOB 'sha256:*' AND length(sha256) = 71),
  requested_time_json TEXT NOT NULL CHECK (json_valid(requested_time_json)),
  source_time_json TEXT NOT NULL CHECK (json_valid(source_time_json)),
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (run_id, turn_id, relative_path)
) STRICT;

CREATE INDEX media_scratch_leases_expiry
ON media_scratch_leases(expires_at, resource_id);

CREATE INDEX media_scratch_leases_turn
ON media_scratch_leases(turn_id, resource_id);
