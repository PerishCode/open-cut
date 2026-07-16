ALTER TABLE work_jobs ADD COLUMN attempt_policy TEXT NOT NULL
  DEFAULT 'local-deterministic-v1'
  CHECK (attempt_policy IN ('local-deterministic-v1'));
