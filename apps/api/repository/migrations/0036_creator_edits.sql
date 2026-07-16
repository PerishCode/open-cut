CREATE TABLE edit_request_identities_v2 (
  actor_kind TEXT NOT NULL CHECK (actor_kind IN ('creator', 'agent')),
  actor_id TEXT NOT NULL CHECK (length(actor_id) = 36),
  request_id TEXT NOT NULL CHECK (length(request_id) BETWEEN 1 AND 128),
  command TEXT NOT NULL CHECK (command IN (
    'edit propose', 'edit apply', 'edit undo',
    'creator edit commit', 'creator edit undo'
  )),
  input_digest TEXT NOT NULL CHECK (input_digest GLOB 'sha256:*' AND length(input_digest) = 71),
  input_json TEXT NOT NULL CHECK (json_valid(input_json)),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  run_id TEXT REFERENCES agent_runs(id) ON DELETE RESTRICT,
  turn_id TEXT REFERENCES agent_turns(id) ON DELETE RESTRICT,
  proposal_id TEXT NOT NULL REFERENCES edit_proposals(id) ON DELETE RESTRICT,
  application_id TEXT REFERENCES proposal_applications(id) ON DELETE RESTRICT,
  transaction_id TEXT REFERENCES edit_transactions(id) ON DELETE RESTRICT,
  activity_event_id TEXT NOT NULL UNIQUE CHECK (length(activity_event_id) = 36),
  created_at TEXT NOT NULL,
  PRIMARY KEY (actor_kind, actor_id, request_id),
  CHECK (
    (
      actor_kind = 'agent' AND run_id IS NOT NULL AND turn_id IS NOT NULL AND
      command IN ('edit propose', 'edit apply', 'edit undo')
    ) OR (
      actor_kind = 'creator' AND run_id IS NULL AND turn_id IS NULL AND
      command IN ('creator edit commit', 'creator edit undo')
    )
  )
) STRICT;

INSERT INTO edit_request_identities_v2 (
  actor_kind, actor_id, request_id, command, input_digest, input_json,
  project_id, run_id, turn_id, proposal_id, application_id, transaction_id,
  activity_event_id, created_at
)
SELECT
  actor_kind, actor_id, request_id, command, input_digest, input_json,
  project_id, run_id, turn_id, proposal_id, application_id, transaction_id,
  activity_event_id, created_at
FROM edit_request_identities;

DROP TABLE edit_request_identities;
ALTER TABLE edit_request_identities_v2 RENAME TO edit_request_identities;
