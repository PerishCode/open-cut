package repository

import (
	"context"
	"database/sql"
	"time"
)

func recoverExpiredWorkAttempts(ctx context.Context, tx *sql.Tx, now time.Time) error {
	at := formatInstant(now.UTC())
	if _, err := tx.ExecContext(ctx, `
DELETE FROM render_material_leases
WHERE attempt_id IN (
  SELECT id FROM work_job_attempts
  WHERE state IN ('leased', 'running', 'publishing') AND lease_expires_at <= ?
)`, at); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE work_job_attempts
SET state = 'abandoned', ended_at = ?, diagnostics_json = '{"code":"lease-expired"}'
WHERE state IN ('leased', 'running', 'publishing') AND lease_expires_at <= ?`, at, at); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
UPDATE work_jobs
SET state = CASE
      WHEN cancellation_requested = 1 THEN 'cancelled'
      WHEN attempt_policy = 'local-deterministic-v1' AND (
        SELECT COALESCE(MAX(generation), 0) FROM work_job_attempts attempt
        WHERE attempt.job_id = work_jobs.id
      ) >= 3 THEN 'failed'
      ELSE 'queued'
    END,
    updated_at = ?,
    terminal_error_code = CASE
      WHEN cancellation_requested = 0
       AND attempt_policy = 'local-deterministic-v1' AND (
        SELECT COALESCE(MAX(generation), 0) FROM work_job_attempts attempt
        WHERE attempt.job_id = work_jobs.id
      ) >= 3 THEN 'attempt-limit-exceeded'
      ELSE NULL
    END
WHERE state = 'running' AND NOT EXISTS (
  SELECT 1 FROM work_job_attempts attempt
  WHERE attempt.job_id = work_jobs.id AND attempt.state IN ('leased', 'running', 'publishing')
)`, at)
	return err
}
