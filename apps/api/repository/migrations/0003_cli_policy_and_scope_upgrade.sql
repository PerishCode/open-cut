ALTER TABLE installation_grants
ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1);

CREATE TABLE cli_invocation_settings (
  singleton INTEGER PRIMARY KEY NOT NULL CHECK (singleton = 1),
  revision INTEGER NOT NULL CHECK (revision >= 1),
  output_mode TEXT NOT NULL CHECK (output_mode IN ('json', 'human')),
  wait_milliseconds INTEGER NOT NULL CHECK (wait_milliseconds BETWEEN 250 AND 30000),
  updated_at TEXT NOT NULL
) STRICT;

INSERT INTO cli_invocation_settings (
  singleton, revision, output_mode, wait_milliseconds, updated_at
) VALUES (1, 1, 'json', 10000, CURRENT_TIMESTAMP);

CREATE TABLE installation_grant_scope_upgrades (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  grant_id TEXT NOT NULL REFERENCES installation_grants(id) ON DELETE RESTRICT,
  from_revision INTEGER NOT NULL CHECK (from_revision >= 1),
  requested_scopes_json TEXT NOT NULL CHECK (json_valid(requested_scopes_json)),
  status TEXT NOT NULL CHECK (status IN ('pending', 'approved', 'denied', 'expired', 'superseded')),
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  decided_at TEXT
) STRICT;

CREATE UNIQUE INDEX installation_grant_scope_upgrades_pending
ON installation_grant_scope_upgrades(grant_id)
WHERE status = 'pending';
