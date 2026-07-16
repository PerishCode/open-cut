ALTER TABLE source_streams RENAME TO source_streams_v6;

CREATE TABLE source_streams (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
  fingerprint TEXT NOT NULL CHECK (fingerprint GLOB 'sha256:*' AND length(fingerprint) = 71),
  descriptor_digest TEXT NOT NULL CHECK (descriptor_digest GLOB 'sha256:*' AND length(descriptor_digest) = 71),
  media_type TEXT NOT NULL CHECK (
    media_type IN ('video', 'audio', 'subtitle', 'data', 'attachment', 'other')
  ),
  descriptor_json TEXT NOT NULL CHECK (json_valid(descriptor_json)),
  UNIQUE (asset_id, fingerprint, descriptor_digest)
) STRICT;

INSERT INTO source_streams (
  id, asset_id, fingerprint, descriptor_digest, media_type, descriptor_json
)
SELECT id, asset_id, fingerprint, descriptor_digest, media_type, descriptor_json
FROM source_streams_v6;

DROP TABLE source_streams_v6;
