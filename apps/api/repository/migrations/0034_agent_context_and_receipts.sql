CREATE TABLE agent_turn_context_attachments (
  turn_id TEXT NOT NULL REFERENCES agent_turns(id) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 63),
  kind TEXT NOT NULL CHECK (
    kind IN (
      'asset', 'transcript-segment', 'narrative-node', 'clip', 'caption',
      'track', 'sequence-point', 'sequence-range'
    )
  ),
  attachment_json TEXT NOT NULL CHECK (
    json_valid(attachment_json) AND length(CAST(attachment_json AS BLOB)) BETWEEN 1 AND 16384
  ),
  PRIMARY KEY (turn_id, ordinal)
) STRICT;

CREATE TABLE agent_command_receipts (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE RESTRICT,
  turn_id TEXT NOT NULL REFERENCES agent_turns(id) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal >= 1),
  actor_id TEXT NOT NULL REFERENCES agent_principals(id) ON DELETE RESTRICT,
  class TEXT NOT NULL CHECK (class IN ('evidence', 'outcome')),
  command TEXT NOT NULL CHECK (length(CAST(command AS BLOB)) BETWEEN 3 AND 128),
  command_fingerprint TEXT NOT NULL CHECK (
    command_fingerprint GLOB 'sha256:*' AND length(command_fingerprint) = 71
  ),
  input_digest TEXT NOT NULL CHECK (
    input_digest GLOB 'sha256:*' AND length(input_digest) = 71
  ),
  request_id TEXT CHECK (
    request_id IS NULL OR length(CAST(request_id AS BLOB)) BETWEEN 1 AND 128
  ),
  result_digest TEXT NOT NULL CHECK (
    result_digest GLOB 'sha256:*' AND length(result_digest) = 71
  ),
  status TEXT NOT NULL CHECK (status IN ('succeeded', 'accepted', 'waiting')),
  project_revision INTEGER CHECK (project_revision IS NULL OR project_revision >= 1),
  activity_cursor INTEGER CHECK (activity_cursor IS NULL OR activity_cursor >= 1),
  created_at TEXT NOT NULL,
  UNIQUE (turn_id, ordinal)
) STRICT;

CREATE INDEX agent_command_receipts_run_turn
ON agent_command_receipts(project_id, run_id, turn_id, ordinal);

CREATE TABLE agent_command_receipt_refs (
  receipt_id TEXT NOT NULL REFERENCES agent_command_receipts(id) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 255),
  kind TEXT NOT NULL CHECK (length(CAST(kind AS BLOB)) BETWEEN 1 AND 64),
  entity_id TEXT NOT NULL CHECK (length(CAST(entity_id AS BLOB)) BETWEEN 1 AND 128),
  entity_revision INTEGER CHECK (entity_revision IS NULL OR entity_revision >= 1),
  PRIMARY KEY (receipt_id, ordinal)
) STRICT;
