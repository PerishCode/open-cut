CREATE TABLE agent_runs (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  intent TEXT NOT NULL CHECK (length(CAST(intent AS BLOB)) BETWEEN 1 AND 4000),
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
  CHECK ((authorization_state = 'pending' AND actor_id IS NULL AND status = 'authorizing') OR
         (authorization_state = 'bound' AND actor_id IS NOT NULL AND status <> 'authorizing')),
  FOREIGN KEY (current_turn_id) REFERENCES agent_turns(id) DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX agent_runs_project_updated ON agent_runs(project_id, updated_at DESC, id);

CREATE TABLE agent_turns (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE RESTRICT,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  generation INTEGER NOT NULL CHECK (generation >= 1),
  adapter TEXT NOT NULL CHECK (length(adapter) BETWEEN 1 AND 64),
  agent_version TEXT NOT NULL CHECK (length(agent_version) BETWEEN 1 AND 128),
  prompt_version TEXT NOT NULL CHECK (length(prompt_version) BETWEEN 1 AND 128),
  native_session_id TEXT,
  status TEXT NOT NULL CHECK (status IN ('starting', 'active', 'detached', 'completed', 'failed', 'cancelled', 'superseded')),
  started_at TEXT NOT NULL,
  ended_at TEXT,
  UNIQUE (run_id, generation)
) STRICT;

CREATE UNIQUE INDEX agent_turns_one_writer
ON agent_turns(run_id)
WHERE status IN ('starting', 'active');

CREATE TABLE run_request_identities (
  actor_id TEXT NOT NULL REFERENCES agent_principals(id) ON DELETE RESTRICT,
  request_id TEXT NOT NULL CHECK (length(request_id) BETWEEN 1 AND 128),
  command TEXT NOT NULL CHECK (command IN ('run begin', 'run resume', 'run complete', 'run cancel')),
  input_digest TEXT NOT NULL CHECK (input_digest GLOB 'sha256:*' AND length(input_digest) = 71),
  input_json TEXT NOT NULL CHECK (json_valid(input_json)),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE RESTRICT,
  turn_id TEXT NOT NULL REFERENCES agent_turns(id) ON DELETE RESTRICT,
  activity_event_id TEXT NOT NULL UNIQUE CHECK (length(activity_event_id) = 36),
  created_at TEXT NOT NULL,
  PRIMARY KEY (actor_id, request_id)
) STRICT;
