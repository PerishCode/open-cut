CREATE TABLE project_versions (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  parent_version_id TEXT REFERENCES project_versions(id) ON DELETE SET NULL,
  captured_project_revision INTEGER NOT NULL CHECK (captured_project_revision >= 1),
  source TEXT NOT NULL CHECK (source IN ('genesis', 'manual', 'agent-turn', 'pre-restore')),
  name TEXT CHECK (name IS NULL OR length(CAST(name AS BLOB)) BETWEEN 1 AND 512),
  trigger_kind TEXT CHECK (trigger_kind IS NULL OR trigger_kind IN ('turn', 'version')),
  trigger_id TEXT CHECK (trigger_id IS NULL OR length(trigger_id) = 36),
  state_schema TEXT NOT NULL CHECK (state_schema = 'open-cut/project-version-state/v1'),
  state_digest TEXT NOT NULL CHECK (state_digest GLOB 'sha256:*' AND length(state_digest) = 71),
  state_bytes BLOB NOT NULL CHECK (length(state_bytes) >= 1),
  state_byte_size INTEGER NOT NULL CHECK (state_byte_size >= 1),
  retention TEXT NOT NULL CHECK (retention IN ('automatic', 'manual', 'pinned')),
  creator_id TEXT NOT NULL REFERENCES local_creators(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  CHECK ((trigger_kind IS NULL) = (trigger_id IS NULL))
) STRICT;

CREATE INDEX project_versions_project_created
ON project_versions(project_id, created_at DESC, id DESC);

CREATE UNIQUE INDEX project_versions_automatic_revision
ON project_versions(project_id, captured_project_revision, source)
WHERE source != 'manual';

CREATE TABLE project_version_requests (
  creator_id TEXT NOT NULL REFERENCES local_creators(id) ON DELETE RESTRICT,
  request_id TEXT NOT NULL CHECK (length(request_id) BETWEEN 1 AND 128),
  command TEXT NOT NULL CHECK (command IN ('create', 'restore')),
  input_digest TEXT NOT NULL CHECK (input_digest GLOB 'sha256:*' AND length(input_digest) = 71),
  input_json TEXT NOT NULL CHECK (json_valid(input_json)),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  version_id TEXT NOT NULL REFERENCES project_versions(id) ON DELETE RESTRICT,
  safety_version_id TEXT REFERENCES project_versions(id) ON DELETE RESTRICT,
  transaction_id TEXT REFERENCES edit_transactions(id) ON DELETE RESTRICT,
  activity_event_id TEXT NOT NULL UNIQUE CHECK (length(activity_event_id) = 36),
  created_at TEXT NOT NULL,
  PRIMARY KEY (creator_id, request_id)
) STRICT;
