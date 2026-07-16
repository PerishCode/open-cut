ALTER TABLE edit_proposals ADD COLUMN request_id TEXT;
ALTER TABLE edit_proposals ADD COLUMN sequence_id TEXT REFERENCES sequences(id) ON DELETE RESTRICT;
ALTER TABLE edit_proposals ADD COLUMN run_id TEXT REFERENCES agent_runs(id) ON DELETE RESTRICT;
ALTER TABLE edit_proposals ADD COLUMN turn_id TEXT REFERENCES agent_turns(id) ON DELETE RESTRICT;
ALTER TABLE edit_proposals ADD COLUMN base_project_revision INTEGER CHECK (base_project_revision IS NULL OR base_project_revision >= 1);
ALTER TABLE edit_proposals ADD COLUMN intent TEXT;
ALTER TABLE edit_proposals ADD COLUMN preconditions_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(preconditions_json));
ALTER TABLE edit_proposals ADD COLUMN allocation_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(allocation_json));
ALTER TABLE edit_proposals ADD COLUMN operations_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(operations_json));
ALTER TABLE edit_proposals ADD COLUMN inverse_preview_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(inverse_preview_json));
ALTER TABLE edit_proposals ADD COLUMN changes_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(changes_json));
ALTER TABLE edit_proposals ADD COLUMN impact_json TEXT NOT NULL DEFAULT '{"classifier":"reversible-local-v1","class":"reversible-local","requiresApproval":false}' CHECK (json_valid(impact_json));
ALTER TABLE edit_proposals ADD COLUMN applied_transaction_id TEXT REFERENCES edit_transactions(id) ON DELETE RESTRICT;
ALTER TABLE edit_proposals ADD COLUMN updated_at TEXT;

ALTER TABLE edit_transactions ADD COLUMN run_id TEXT REFERENCES agent_runs(id) ON DELETE RESTRICT;
ALTER TABLE edit_transactions ADD COLUMN turn_id TEXT REFERENCES agent_turns(id) ON DELETE RESTRICT;
ALTER TABLE edit_transactions ADD COLUMN intent TEXT;
ALTER TABLE edit_transactions ADD COLUMN digest TEXT CHECK (digest IS NULL OR (digest GLOB 'sha256:*' AND length(digest) = 71));
ALTER TABLE edit_transactions ADD COLUMN changes_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(changes_json));
ALTER TABLE edit_transactions ADD COLUMN undoes_transaction_id TEXT REFERENCES edit_transactions(id) ON DELETE RESTRICT;

CREATE TABLE narrative_authored_texts (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  document_id TEXT NOT NULL REFERENCES narrative_documents(id) ON DELETE RESTRICT,
  parent_id TEXT NOT NULL REFERENCES narrative_nodes(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision >= 1),
  text TEXT NOT NULL CHECK (length(CAST(text AS BLOB)) BETWEEN 1 AND 262144),
  order_index INTEGER NOT NULL CHECK (order_index >= 0),
  tombstoned INTEGER NOT NULL CHECK (tombstoned IN (0, 1)),
  last_transaction_id TEXT NOT NULL REFERENCES edit_transactions(id) DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX narrative_authored_texts_page
ON narrative_authored_texts(document_id, parent_id, order_index, id);

CREATE TABLE captions (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  sequence_id TEXT NOT NULL REFERENCES sequences(id) ON DELETE RESTRICT,
  track_id TEXT NOT NULL REFERENCES tracks(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision >= 1),
  start_value INTEGER NOT NULL,
  start_scale INTEGER NOT NULL CHECK (start_scale BETWEEN 1 AND 2147483647),
  duration_value INTEGER NOT NULL CHECK (duration_value > 0),
  duration_scale INTEGER NOT NULL CHECK (duration_scale BETWEEN 1 AND 2147483647),
  start_order_key TEXT NOT NULL CHECK (length(start_order_key) = 48),
  end_order_key TEXT NOT NULL CHECK (length(end_order_key) = 48),
  text TEXT NOT NULL CHECK (length(CAST(text AS BLOB)) BETWEEN 1 AND 262144),
  tombstoned INTEGER NOT NULL CHECK (tombstoned IN (0, 1)),
  last_transaction_id TEXT NOT NULL REFERENCES edit_transactions(id) DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX captions_window
ON captions(sequence_id, track_id, tombstoned, start_order_key, end_order_key, id);

CREATE TABLE caption_alignments (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  narrative_node_id TEXT NOT NULL REFERENCES narrative_authored_texts(id) ON DELETE RESTRICT,
  narrative_node_revision INTEGER NOT NULL CHECK (narrative_node_revision >= 1),
  sequence_id TEXT NOT NULL REFERENCES sequences(id) ON DELETE RESTRICT,
  caption_id TEXT NOT NULL REFERENCES captions(id) ON DELETE RESTRICT,
  caption_revision INTEGER NOT NULL CHECK (caption_revision >= 1),
  local_start_value INTEGER NOT NULL,
  local_start_scale INTEGER NOT NULL CHECK (local_start_scale BETWEEN 1 AND 2147483647),
  local_duration_value INTEGER NOT NULL CHECK (local_duration_value > 0),
  local_duration_scale INTEGER NOT NULL CHECK (local_duration_scale BETWEEN 1 AND 2147483647),
  revision INTEGER NOT NULL CHECK (revision >= 1),
  status TEXT NOT NULL CHECK (status IN ('exact', 'stale', 'unbound')),
  last_transaction_id TEXT NOT NULL REFERENCES edit_transactions(id) DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX caption_alignments_node ON caption_alignments(project_id, narrative_node_id, id);
CREATE INDEX caption_alignments_caption ON caption_alignments(project_id, caption_id, id);

CREATE TABLE proposal_applications (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  proposal_id TEXT NOT NULL REFERENCES edit_proposals(id) ON DELETE RESTRICT,
  actor_kind TEXT NOT NULL CHECK (actor_kind IN ('creator', 'agent')),
  actor_id TEXT NOT NULL CHECK (length(actor_id) = 36),
  request_id TEXT NOT NULL CHECK (length(request_id) BETWEEN 1 AND 128),
  input_digest TEXT NOT NULL CHECK (input_digest GLOB 'sha256:*' AND length(input_digest) = 71),
  status TEXT NOT NULL CHECK (status IN ('approval-pending', 'committed', 'denied', 'expired', 'stale')),
  transaction_id TEXT REFERENCES edit_transactions(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (actor_kind, actor_id, request_id)
) STRICT;

CREATE UNIQUE INDEX proposal_applications_one_pending
ON proposal_applications(proposal_id)
WHERE status = 'approval-pending';

CREATE TABLE edit_request_identities (
  actor_kind TEXT NOT NULL CHECK (actor_kind IN ('creator', 'agent')),
  actor_id TEXT NOT NULL CHECK (length(actor_id) = 36),
  request_id TEXT NOT NULL CHECK (length(request_id) BETWEEN 1 AND 128),
  command TEXT NOT NULL CHECK (command IN ('edit propose', 'edit apply', 'edit undo')),
  input_digest TEXT NOT NULL CHECK (input_digest GLOB 'sha256:*' AND length(input_digest) = 71),
  input_json TEXT NOT NULL CHECK (json_valid(input_json)),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE RESTRICT,
  turn_id TEXT NOT NULL REFERENCES agent_turns(id) ON DELETE RESTRICT,
  proposal_id TEXT NOT NULL REFERENCES edit_proposals(id) ON DELETE RESTRICT,
  application_id TEXT REFERENCES proposal_applications(id) ON DELETE RESTRICT,
  transaction_id TEXT REFERENCES edit_transactions(id) ON DELETE RESTRICT,
  activity_event_id TEXT NOT NULL UNIQUE CHECK (length(activity_event_id) = 36),
  created_at TEXT NOT NULL,
  PRIMARY KEY (actor_kind, actor_id, request_id)
) STRICT;

CREATE TABLE agent_run_transactions (
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE RESTRICT,
  transaction_id TEXT NOT NULL UNIQUE REFERENCES edit_transactions(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  PRIMARY KEY (run_id, transaction_id)
) STRICT;
