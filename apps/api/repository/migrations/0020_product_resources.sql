CREATE TABLE product_resources (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  installation_id TEXT NOT NULL CHECK (
    length(CAST(installation_id AS BLOB)) BETWEEN 1 AND 128
  ),
  catalog_entry_id TEXT NOT NULL CHECK (
    length(CAST(catalog_entry_id AS BLOB)) BETWEEN 1 AND 128
  ),
  kind TEXT NOT NULL CHECK (kind IN ('transcription-model')),
  version TEXT NOT NULL CHECK (length(CAST(version AS BLOB)) BETWEEN 1 AND 128),
  profile TEXT NOT NULL CHECK (length(CAST(profile AS BLOB)) BETWEEN 1 AND 128),
  entry_digest TEXT NOT NULL CHECK (
    entry_digest GLOB 'sha256:*' AND length(entry_digest) = 71
  ),
  content_digest TEXT NOT NULL CHECK (
    content_digest GLOB 'sha256:*' AND length(content_digest) = 71
  ),
  state TEXT NOT NULL CHECK (state IN ('ready', 'invalid')),
  byte_size INTEGER NOT NULL CHECK (byte_size > 0),
  byte_reference TEXT NOT NULL UNIQUE CHECK (
    length(CAST(byte_reference AS BLOB)) BETWEEN 1 AND 256
  ),
  retention TEXT NOT NULL CHECK (retention IN ('offline')),
  producer_job_id TEXT NOT NULL UNIQUE REFERENCES work_jobs(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  UNIQUE (installation_id, catalog_entry_id, entry_digest)
) STRICT;

CREATE INDEX product_resources_requirement
ON product_resources(installation_id, catalog_entry_id, profile, entry_digest);

CREATE TABLE resource_job_details (
  job_id TEXT PRIMARY KEY NOT NULL REFERENCES work_jobs(id) ON DELETE RESTRICT,
  catalog_entry_id TEXT NOT NULL CHECK (
    length(CAST(catalog_entry_id AS BLOB)) BETWEEN 1 AND 128
  ),
  resource_kind TEXT NOT NULL CHECK (resource_kind IN ('transcription-model')),
  resource_version TEXT NOT NULL CHECK (
    length(CAST(resource_version AS BLOB)) BETWEEN 1 AND 128
  ),
  resource_profile TEXT NOT NULL CHECK (
    length(CAST(resource_profile AS BLOB)) BETWEEN 1 AND 128
  ),
  entry_digest TEXT NOT NULL CHECK (
    entry_digest GLOB 'sha256:*' AND length(entry_digest) = 71
  ),
  entry_json TEXT NOT NULL CHECK (json_valid(entry_json)),
  origin TEXT NOT NULL CHECK (length(CAST(origin AS BLOB)) BETWEEN 1 AND 4096),
  expected_byte_size INTEGER NOT NULL CHECK (expected_byte_size > 0),
  expected_content_digest TEXT NOT NULL CHECK (
    expected_content_digest GLOB 'sha256:*' AND length(expected_content_digest) = 71
  ),
  retention TEXT NOT NULL CHECK (retention IN ('offline')),
  result_resource_id TEXT REFERENCES product_resources(id) ON DELETE RESTRICT
) STRICT;

CREATE INDEX resource_job_details_entry
ON resource_job_details(catalog_entry_id, entry_digest, job_id);

CREATE TABLE product_resource_requests (
  installation_id TEXT NOT NULL CHECK (
    length(CAST(installation_id AS BLOB)) BETWEEN 1 AND 128
  ),
  request_id TEXT NOT NULL CHECK (
    length(CAST(request_id AS BLOB)) BETWEEN 1 AND 128
  ),
  input_digest TEXT NOT NULL CHECK (
    input_digest GLOB 'sha256:*' AND length(input_digest) = 71
  ),
  input_json TEXT NOT NULL CHECK (json_valid(input_json)),
  job_id TEXT NOT NULL REFERENCES work_jobs(id) ON DELETE RESTRICT,
  activity_event_id TEXT NOT NULL UNIQUE CHECK (length(activity_event_id) = 36),
  created_at TEXT NOT NULL,
  PRIMARY KEY (installation_id, request_id)
) STRICT;
