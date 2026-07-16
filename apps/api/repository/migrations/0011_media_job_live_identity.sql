DROP INDEX media_jobs_live_logical_key;

CREATE UNIQUE INDEX media_jobs_live_logical_key
ON media_jobs(logical_key)
WHERE state IN ('blocked', 'queued', 'running');
