DROP TABLE project_state;
DROP TABLE projects;

CREATE TABLE local_creators (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  singleton INTEGER NOT NULL UNIQUE CHECK (singleton = 1),
  created_at TEXT NOT NULL
) STRICT;

CREATE TABLE agent_principals (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  created_at TEXT NOT NULL
) STRICT;

CREATE TABLE projects (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  revision INTEGER NOT NULL CHECK (revision >= 1),
  lifecycle_revision INTEGER NOT NULL CHECK (lifecycle_revision >= 1),
  name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
  status TEXT NOT NULL CHECK (status IN ('active', 'archived', 'tombstoned')),
  narrative_document_id TEXT NOT NULL CHECK (length(narrative_document_id) = 36),
  main_sequence_id TEXT NOT NULL CHECK (length(main_sequence_id) = 36),
  creator_id TEXT NOT NULL REFERENCES local_creators(id),
  created_at TEXT NOT NULL,
  FOREIGN KEY (narrative_document_id) REFERENCES narrative_documents(id) DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (main_sequence_id) REFERENCES sequences(id) DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX projects_status_id ON projects(status, id);

CREATE TABLE narrative_documents (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision >= 1),
  kind TEXT NOT NULL CHECK (kind = 'paper-edit'),
  root_node_id TEXT NOT NULL CHECK (length(root_node_id) = 36),
  FOREIGN KEY (root_node_id) REFERENCES narrative_nodes(id) DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE UNIQUE INDEX narrative_documents_project_kind ON narrative_documents(project_id, kind);

CREATE TABLE narrative_nodes (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  document_id TEXT NOT NULL REFERENCES narrative_documents(id) ON DELETE RESTRICT,
  parent_id TEXT REFERENCES narrative_nodes(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision >= 1),
  kind TEXT NOT NULL CHECK (kind = 'section'),
  title TEXT NOT NULL,
  order_key TEXT NOT NULL CHECK (length(order_key) BETWEEN 1 AND 128)
) STRICT;

CREATE UNIQUE INDEX narrative_nodes_sibling_order ON narrative_nodes(document_id, parent_id, order_key);

CREATE TABLE sequences (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision >= 1),
  name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
  role TEXT NOT NULL CHECK (role = 'main'),
  canvas_width INTEGER NOT NULL CHECK (canvas_width BETWEEN 16 AND 16384),
  canvas_height INTEGER NOT NULL CHECK (canvas_height BETWEEN 16 AND 16384),
  pixel_aspect_value INTEGER NOT NULL,
  pixel_aspect_scale INTEGER NOT NULL CHECK (pixel_aspect_scale BETWEEN 1 AND 2147483647),
  frame_rate_value INTEGER NOT NULL,
  frame_rate_scale INTEGER NOT NULL CHECK (frame_rate_scale BETWEEN 1 AND 2147483647),
  audio_sample_rate INTEGER NOT NULL CHECK (audio_sample_rate BETWEEN 8000 AND 384000),
  audio_layout TEXT NOT NULL CHECK (audio_layout = 'stereo'),
  color_policy TEXT NOT NULL CHECK (color_policy = 'sdr-rec709')
) STRICT;

CREATE UNIQUE INDEX sequences_project_role ON sequences(project_id, role);

CREATE TABLE tracks (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  sequence_id TEXT NOT NULL REFERENCES sequences(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision >= 1),
  type TEXT NOT NULL CHECK (type IN ('video', 'audio', 'caption')),
  label TEXT NOT NULL CHECK (length(label) BETWEEN 1 AND 200),
  order_key TEXT NOT NULL CHECK (length(order_key) BETWEEN 1 AND 128)
) STRICT;

CREATE UNIQUE INDEX tracks_sequence_order ON tracks(sequence_id, order_key);

CREATE TABLE edit_proposals (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  schema_version TEXT NOT NULL,
  digest TEXT NOT NULL CHECK (length(digest) = 71),
  canonical_json TEXT NOT NULL CHECK (json_valid(canonical_json)),
  actor_kind TEXT NOT NULL CHECK (actor_kind IN ('creator', 'agent')),
  actor_id TEXT NOT NULL CHECK (length(actor_id) = 36),
  status TEXT NOT NULL CHECK (status IN ('pending', 'approval-pending', 'applied', 'cancelled', 'stale')),
  created_at TEXT NOT NULL
) STRICT;

CREATE INDEX edit_proposals_project_created ON edit_proposals(project_id, created_at, id);

CREATE TABLE edit_transactions (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  proposal_id TEXT NOT NULL UNIQUE REFERENCES edit_proposals(id) ON DELETE RESTRICT,
  project_revision INTEGER NOT NULL CHECK (project_revision >= 1),
  schema_version TEXT NOT NULL,
  operation_json TEXT NOT NULL CHECK (json_valid(operation_json)),
  inverse_json TEXT NOT NULL CHECK (json_valid(inverse_json)),
  actor_kind TEXT NOT NULL CHECK (actor_kind IN ('creator', 'agent')),
  actor_id TEXT NOT NULL CHECK (length(actor_id) = 36),
  committed_at TEXT NOT NULL
) STRICT;

CREATE UNIQUE INDEX edit_transactions_project_revision ON edit_transactions(project_id, project_revision);

CREATE TABLE request_identities (
  actor_kind TEXT NOT NULL CHECK (actor_kind IN ('creator', 'agent')),
  actor_id TEXT NOT NULL CHECK (length(actor_id) = 36),
  request_id TEXT NOT NULL CHECK (length(request_id) BETWEEN 1 AND 128),
  schema_version TEXT NOT NULL,
  input_digest TEXT NOT NULL CHECK (length(input_digest) = 71),
  input_json TEXT NOT NULL CHECK (json_valid(input_json)),
  installation_id TEXT NOT NULL CHECK (length(installation_id) BETWEEN 1 AND 128),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  proposal_id TEXT NOT NULL REFERENCES edit_proposals(id) ON DELETE RESTRICT,
  transaction_id TEXT NOT NULL REFERENCES edit_transactions(id) ON DELETE RESTRICT,
  project_activity_event_id TEXT NOT NULL UNIQUE CHECK (length(project_activity_event_id) = 36),
  installation_activity_event_id TEXT NOT NULL UNIQUE CHECK (length(installation_activity_event_id) = 36),
  status TEXT NOT NULL CHECK (status IN ('committed', 'terminal')),
  created_at TEXT NOT NULL,
  PRIMARY KEY (actor_kind, actor_id, request_id)
) STRICT;

CREATE TABLE activity_heads (
  scope_kind TEXT NOT NULL CHECK (scope_kind IN ('project', 'installation')),
  scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
  cursor INTEGER NOT NULL CHECK (cursor >= 0),
  PRIMARY KEY (scope_kind, scope_id)
) STRICT;

CREATE TABLE activity_outbox (
  scope_kind TEXT NOT NULL CHECK (scope_kind IN ('project', 'installation')),
  scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
  cursor INTEGER NOT NULL CHECK (cursor >= 1),
  event_id TEXT NOT NULL UNIQUE CHECK (length(event_id) = 36),
  schema_version TEXT NOT NULL,
  kind TEXT NOT NULL,
  occurred_at TEXT NOT NULL,
  actor_kind TEXT CHECK (actor_kind IN ('creator', 'agent')),
  actor_id TEXT CHECK (actor_id IS NULL OR length(actor_id) = 36),
  project_id TEXT REFERENCES projects(id) ON DELETE RESTRICT,
  project_revision INTEGER CHECK (project_revision IS NULL OR project_revision >= 1),
  outcome_kind TEXT,
  outcome_id TEXT,
  summary_code TEXT NOT NULL,
  payload_json TEXT NOT NULL CHECK (json_valid(payload_json)),
  PRIMARY KEY (scope_kind, scope_id, cursor),
  FOREIGN KEY (scope_kind, scope_id) REFERENCES activity_heads(scope_kind, scope_id) ON DELETE RESTRICT
) STRICT;

CREATE TABLE installation_grants (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  installation_id TEXT NOT NULL CHECK (length(installation_id) BETWEEN 1 AND 128),
  agent_id TEXT NOT NULL REFERENCES agent_principals(id) ON DELETE RESTRICT,
  role TEXT NOT NULL CHECK (role = 'product-cli'),
  algorithm TEXT NOT NULL CHECK (algorithm = 'ed25519'),
  public_key TEXT NOT NULL,
  public_key_fingerprint TEXT NOT NULL CHECK (public_key_fingerprint GLOB 'sha256:*' AND length(public_key_fingerprint) = 71),
  scopes_json TEXT NOT NULL CHECK (json_valid(scopes_json)),
  status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'denied', 'revoked', 'expired')),
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  decided_at TEXT,
  revoked_at TEXT
) STRICT;

CREATE UNIQUE INDEX installation_grants_public_key ON installation_grants(installation_id, role, public_key);

CREATE TABLE authorization_audit (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  installation_id TEXT NOT NULL CHECK (length(installation_id) BETWEEN 1 AND 128),
  principal_kind TEXT NOT NULL CHECK (principal_kind IN ('first-party-ui', 'product-cli')),
  principal_id TEXT NOT NULL,
  action TEXT NOT NULL,
  outcome TEXT NOT NULL,
  request_digest TEXT,
  occurred_at TEXT NOT NULL
) STRICT;
