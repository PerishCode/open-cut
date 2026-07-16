CREATE TABLE agent_runs_v2 (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  intent TEXT NOT NULL CHECK (length(CAST(intent AS BLOB)) BETWEEN 1 AND 32768),
  initiator_kind TEXT NOT NULL CHECK (initiator_kind IN ('creator', 'agent')),
  initiator_id TEXT NOT NULL CHECK (length(initiator_id) = 36),
  actor_id TEXT REFERENCES agent_principals(id) ON DELETE RESTRICT,
  authorization_state TEXT NOT NULL CHECK (authorization_state IN ('pending', 'bound')),
  status TEXT NOT NULL CHECK (status IN ('authorizing', 'active', 'waiting', 'paused', 'completed', 'failed', 'cancelled')),
  waiting_reason TEXT,
  started_project_revision INTEGER NOT NULL CHECK (started_project_revision >= 1),
  latest_observed_project_revision INTEGER NOT NULL CHECK (latest_observed_project_revision >= 1),
  current_turn_id TEXT NOT NULL CHECK (length(current_turn_id) = 36),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  completed_at TEXT,
  CHECK (
    (authorization_state = 'pending' AND actor_id IS NULL AND status IN ('authorizing', 'paused', 'failed', 'cancelled')) OR
    (authorization_state = 'bound' AND actor_id IS NOT NULL AND status <> 'authorizing')
  ),
  FOREIGN KEY (current_turn_id) REFERENCES agent_turns(id) DEFERRABLE INITIALLY DEFERRED
) STRICT;

INSERT INTO agent_runs_v2 (
  id, project_id, intent, initiator_kind, initiator_id, actor_id, authorization_state,
  status, waiting_reason, started_project_revision, latest_observed_project_revision,
  current_turn_id, created_at, updated_at, completed_at
)
SELECT id, project_id, intent, initiator_kind, initiator_id, actor_id, authorization_state,
       status, waiting_reason, started_project_revision, latest_observed_project_revision,
       current_turn_id, created_at, updated_at, completed_at
FROM agent_runs;

DROP INDEX agent_runs_project_updated;
DROP TABLE agent_runs;
ALTER TABLE agent_runs_v2 RENAME TO agent_runs;

CREATE INDEX agent_runs_project_updated ON agent_runs(project_id, updated_at DESC, id);

CREATE TABLE agent_bridge_runs (
  run_id TEXT PRIMARY KEY NOT NULL REFERENCES agent_runs(id) ON DELETE RESTRICT,
  adapter_id TEXT NOT NULL CHECK (adapter_id = 'codex-cli-v1'),
  created_at TEXT NOT NULL
) STRICT;

CREATE TABLE agent_bridge_turns (
  turn_id TEXT PRIMARY KEY NOT NULL REFERENCES agent_turns(id) ON DELETE RESTRICT,
  sequence_id TEXT REFERENCES sequences(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL
) STRICT;

CREATE TABLE agent_conversation_messages (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE RESTRICT,
  turn_id TEXT NOT NULL REFERENCES agent_turns(id) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal >= 1),
  role TEXT NOT NULL CHECK (role IN ('creator', 'agent', 'notice')),
  text TEXT NOT NULL CHECK (length(CAST(text AS BLOB)) BETWEEN 1 AND 262144),
  created_at TEXT NOT NULL,
  UNIQUE (run_id, ordinal)
) STRICT;

CREATE INDEX agent_conversation_messages_turn
ON agent_conversation_messages(turn_id, ordinal);

CREATE TABLE agent_bridge_requests (
  creator_id TEXT NOT NULL REFERENCES local_creators(id) ON DELETE RESTRICT,
  request_id TEXT NOT NULL CHECK (length(CAST(request_id AS BLOB)) BETWEEN 1 AND 128),
  command TEXT NOT NULL CHECK (command IN ('begin', 'continue', 'interrupt', 'cancel')),
  input_digest TEXT NOT NULL CHECK (input_digest GLOB 'sha256:*' AND length(input_digest) = 71),
  input_json TEXT NOT NULL CHECK (json_valid(input_json)),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE RESTRICT,
  turn_id TEXT NOT NULL REFERENCES agent_turns(id) ON DELETE RESTRICT,
  message_id TEXT REFERENCES agent_conversation_messages(id) ON DELETE RESTRICT,
  activity_event_id TEXT NOT NULL REFERENCES activity_outbox(event_id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  PRIMARY KEY (creator_id, request_id)
) STRICT;

CREATE TABLE agent_run_pairing_associations (
  run_id TEXT PRIMARY KEY NOT NULL REFERENCES agent_runs(id) ON DELETE RESTRICT,
  originating_turn_id TEXT NOT NULL REFERENCES agent_turns(id) ON DELETE RESTRICT,
  grant_id TEXT NOT NULL REFERENCES installation_grants(id) ON DELETE RESTRICT,
  grant_revision INTEGER NOT NULL CHECK (grant_revision >= 1),
  created_at TEXT NOT NULL
) STRICT;

CREATE UNIQUE INDEX agent_run_pairing_grant_revision
ON agent_run_pairing_associations(grant_id, grant_revision);
