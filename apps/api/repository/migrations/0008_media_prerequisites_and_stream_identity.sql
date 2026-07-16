CREATE TABLE media_job_prerequisites (
  job_id TEXT NOT NULL REFERENCES media_jobs(id) ON DELETE RESTRICT,
  kind TEXT NOT NULL CHECK (
    kind IN ('fingerprint-required', 'facts-required', 'model-required', 'executor-required')
  ),
  reference_kind TEXT NOT NULL CHECK (reference_kind IN ('job', 'resource', 'capability')),
  reference_id TEXT NOT NULL CHECK (length(CAST(reference_id AS BLOB)) BETWEEN 1 AND 256),
  created_at TEXT NOT NULL,
  PRIMARY KEY (job_id, kind, reference_kind, reference_id),
  CHECK (
    (kind IN ('fingerprint-required', 'facts-required') AND reference_kind = 'job') OR
    (kind = 'model-required' AND reference_kind = 'resource') OR
    (kind = 'executor-required' AND reference_kind = 'capability')
  )
) STRICT;

CREATE INDEX media_job_prerequisites_reference
ON media_job_prerequisites(reference_kind, reference_id, job_id);

INSERT INTO media_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
SELECT dependent.id, 'fingerprint-required', 'job', producer.id, dependent.created_at
FROM media_jobs dependent
JOIN media_jobs producer ON producer.asset_id = dependent.asset_id AND producer.kind = 'identify'
WHERE dependent.kind = 'probe' AND dependent.state IN ('blocked', 'queued');

INSERT INTO media_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
SELECT dependent.id, 'facts-required', 'job', producer.id, dependent.created_at
FROM media_jobs dependent
JOIN media_jobs producer ON producer.asset_id = dependent.asset_id AND producer.kind = 'probe'
WHERE dependent.kind IN ('frame-sample-set', 'proxy', 'waveform', 'transcript')
  AND dependent.state IN ('blocked', 'queued');

INSERT INTO media_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
SELECT id, 'model-required', 'resource', 'whisper-small-multilingual-v1', created_at
FROM media_jobs
WHERE kind = 'transcript' AND state IN ('blocked', 'queued');

INSERT INTO media_job_prerequisites (job_id, kind, reference_kind, reference_id, created_at)
SELECT id, 'executor-required', 'capability', 'media-executor/' || kind, created_at
FROM media_jobs
WHERE state IN ('blocked', 'queued');

UPDATE media_jobs
SET state = 'blocked'
WHERE state = 'queued'
  AND EXISTS (SELECT 1 FROM media_job_prerequisites prerequisite WHERE prerequisite.job_id = media_jobs.id);

ALTER TABLE media_jobs DROP COLUMN blocked_reason;

ALTER TABLE source_streams RENAME TO source_streams_v7;

CREATE TABLE source_streams (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
  fingerprint TEXT NOT NULL CHECK (fingerprint GLOB 'sha256:*' AND length(fingerprint) = 71),
  container_index INTEGER NOT NULL CHECK (container_index BETWEEN 0 AND 4294967295),
  descriptor_digest TEXT NOT NULL CHECK (descriptor_digest GLOB 'sha256:*' AND length(descriptor_digest) = 71),
  media_type TEXT NOT NULL CHECK (
    media_type IN ('video', 'audio', 'subtitle', 'data', 'attachment', 'other')
  ),
  descriptor_json TEXT NOT NULL CHECK (json_valid(descriptor_json)),
  UNIQUE (asset_id, fingerprint, container_index)
) STRICT;

INSERT INTO source_streams (
  id, asset_id, fingerprint, container_index, descriptor_digest, media_type, descriptor_json
)
SELECT id, asset_id, fingerprint, CAST(json_extract(descriptor_json, '$.index') AS INTEGER),
       descriptor_digest, media_type, descriptor_json
FROM source_streams_v7;

DROP TABLE source_streams_v7;
