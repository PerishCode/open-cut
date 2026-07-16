CREATE TEMP TABLE obsolete_initial_frame_jobs (
  id TEXT PRIMARY KEY NOT NULL
) WITHOUT ROWID;

INSERT INTO obsolete_initial_frame_jobs (id)
SELECT id
FROM media_jobs
WHERE kind = 'frame-sample-set'
  AND producer_version = 'open-cut-media-v1'
  AND json_extract(parameters_json, '$.payload.profile') = 'initial-v1'
  AND result_artifact_id IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM media_job_attempts attempt WHERE attempt.job_id = media_jobs.id
  );

UPDATE activity_outbox
SET payload_json = json_set(
  payload_json,
  '$.jobIds',
  json(COALESCE((
    SELECT json_group_array(value)
    FROM (
      SELECT item.value AS value
      FROM json_each(activity_outbox.payload_json, '$.jobIds') item
      LEFT JOIN obsolete_initial_frame_jobs obsolete ON obsolete.id = item.value
      WHERE obsolete.id IS NULL
      ORDER BY CAST(item.key AS INTEGER)
    ) retained
  ), '[]'))
)
WHERE json_type(payload_json, '$.jobIds') = 'array'
  AND EXISTS (
    SELECT 1
    FROM json_each(activity_outbox.payload_json, '$.jobIds') item
    JOIN obsolete_initial_frame_jobs obsolete ON obsolete.id = item.value
  );

DELETE FROM media_job_prerequisites
WHERE job_id IN (SELECT id FROM obsolete_initial_frame_jobs);

DELETE FROM media_job_owners
WHERE job_id IN (SELECT id FROM obsolete_initial_frame_jobs);

DELETE FROM media_jobs
WHERE id IN (SELECT id FROM obsolete_initial_frame_jobs);

DROP TABLE obsolete_initial_frame_jobs;
