PRAGMA defer_foreign_keys = ON;

-- Narrative leaves share one identity, revision, sibling order, and tombstone
-- model. Typed payload tables prevent authored text and source excerpts from
-- becoming independent, ambiguously ordered document projections.
CREATE TABLE narrative_leaf_nodes (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  document_id TEXT NOT NULL REFERENCES narrative_documents(id) ON DELETE RESTRICT,
  parent_id TEXT NOT NULL REFERENCES narrative_nodes(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision >= 1),
  kind TEXT NOT NULL CHECK (kind IN ('authored-text', 'source-excerpt')),
  order_index INTEGER NOT NULL CHECK (order_index >= 0),
  tombstoned INTEGER NOT NULL CHECK (tombstoned IN (0, 1)),
  last_transaction_id TEXT NOT NULL REFERENCES edit_transactions(id) DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX narrative_leaf_nodes_page
ON narrative_leaf_nodes(document_id, parent_id, tombstoned, order_index, id);

CREATE TABLE narrative_authored_text_values (
  id TEXT PRIMARY KEY NOT NULL REFERENCES narrative_leaf_nodes(id) ON DELETE RESTRICT,
  text TEXT NOT NULL CHECK (length(CAST(text AS BLOB)) BETWEEN 1 AND 262144)
) STRICT;

INSERT INTO narrative_leaf_nodes (
  id, project_id, document_id, parent_id, revision, kind, order_index,
  tombstoned, last_transaction_id
)
SELECT id, project_id, document_id, parent_id, revision, 'authored-text',
       order_index, tombstoned, last_transaction_id
FROM narrative_authored_texts;

INSERT INTO narrative_authored_text_values (id, text)
SELECT id, text FROM narrative_authored_texts;

-- Alignment identity targets the common Narrative leaf, not one payload kind.
CREATE TABLE caption_alignments_v2 (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  narrative_node_id TEXT NOT NULL REFERENCES narrative_leaf_nodes(id) ON DELETE RESTRICT,
  narrative_node_revision INTEGER NOT NULL CHECK (narrative_node_revision >= 1),
  sequence_id TEXT NOT NULL REFERENCES sequences(id) ON DELETE RESTRICT,
  caption_id TEXT NOT NULL REFERENCES captions(id) ON DELETE RESTRICT,
  caption_revision INTEGER NOT NULL CHECK (caption_revision >= 1),
  local_start_value INTEGER NOT NULL,
  local_start_scale INTEGER NOT NULL CHECK (local_start_scale BETWEEN 1 AND 2147483647),
  local_duration_value INTEGER NOT NULL CHECK (local_duration_value > 0),
  local_duration_scale INTEGER NOT NULL CHECK (local_duration_scale BETWEEN 1 AND 2147483647),
  revision INTEGER NOT NULL CHECK (revision >= 1),
  status TEXT NOT NULL CHECK (status IN ('exact', 'stale', 'unbound')),
  last_transaction_id TEXT NOT NULL REFERENCES edit_transactions(id) DEFERRABLE INITIALLY DEFERRED
) STRICT;

INSERT INTO caption_alignments_v2 (
  id, project_id, narrative_node_id, narrative_node_revision, sequence_id,
  caption_id, caption_revision, local_start_value, local_start_scale,
  local_duration_value, local_duration_scale, revision, status,
  last_transaction_id
)
SELECT id, project_id, narrative_node_id, narrative_node_revision, sequence_id,
       caption_id, caption_revision, local_start_value, local_start_scale,
       local_duration_value, local_duration_scale, revision, status,
       last_transaction_id
FROM caption_alignments;

DROP TABLE caption_alignments;
DROP TABLE narrative_authored_texts;
ALTER TABLE caption_alignments_v2 RENAME TO caption_alignments;

CREATE INDEX caption_alignments_node
ON caption_alignments(project_id, narrative_node_id, id);
CREATE INDEX caption_alignments_caption
ON caption_alignments(project_id, caption_id, id);

CREATE TABLE transcript_corrections (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
  artifact_id TEXT NOT NULL REFERENCES transcript_artifacts(artifact_id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision >= 1),
  source_start_value INTEGER NOT NULL,
  source_start_scale INTEGER NOT NULL CHECK (source_start_scale BETWEEN 1 AND 2147483647),
  source_duration_value INTEGER NOT NULL CHECK (source_duration_value > 0),
  source_duration_scale INTEGER NOT NULL CHECK (source_duration_scale BETWEEN 1 AND 2147483647),
  source_start_order_key TEXT NOT NULL CHECK (length(source_start_order_key) = 48),
  source_end_order_key TEXT NOT NULL CHECK (length(source_end_order_key) = 48),
  replacement_text TEXT NOT NULL CHECK (
    length(CAST(replacement_text AS BLOB)) BETWEEN 1 AND 262144
  ),
  language TEXT NOT NULL CHECK (
    length(CAST(language AS BLOB)) BETWEEN 2 AND 64 AND
    language NOT GLOB '*[^A-Za-z0-9-]*'
  ),
  tombstoned INTEGER NOT NULL CHECK (tombstoned IN (0, 1)),
  last_transaction_id TEXT NOT NULL REFERENCES edit_transactions(id) DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX transcript_corrections_overlap
ON transcript_corrections(
  artifact_id, language, tombstoned, source_start_order_key, source_end_order_key, id
);

CREATE TABLE transcript_correction_segments (
  correction_id TEXT NOT NULL REFERENCES transcript_corrections(id) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 255),
  segment_id TEXT NOT NULL REFERENCES transcript_segments(id) ON DELETE RESTRICT,
  PRIMARY KEY (correction_id, ordinal),
  UNIQUE (correction_id, segment_id)
) STRICT;

CREATE TABLE narrative_source_excerpt_values (
  id TEXT PRIMARY KEY NOT NULL REFERENCES narrative_leaf_nodes(id) ON DELETE RESTRICT,
  asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
  accepted_fingerprint TEXT NOT NULL CHECK (
    accepted_fingerprint GLOB 'sha256:*' AND length(accepted_fingerprint) = 71
  ),
  source_start_value INTEGER NOT NULL,
  source_start_scale INTEGER NOT NULL CHECK (source_start_scale BETWEEN 1 AND 2147483647),
  source_duration_value INTEGER NOT NULL CHECK (source_duration_value > 0),
  source_duration_scale INTEGER NOT NULL CHECK (source_duration_scale BETWEEN 1 AND 2147483647),
  language TEXT NOT NULL CHECK (
    length(CAST(language AS BLOB)) BETWEEN 2 AND 64 AND
    language NOT GLOB '*[^A-Za-z0-9-]*'
  ),
  effective_text TEXT NOT NULL CHECK (
    length(CAST(effective_text AS BLOB)) BETWEEN 1 AND 262144
  ),
  transcript_artifact_id TEXT NOT NULL REFERENCES transcript_artifacts(artifact_id) ON DELETE RESTRICT,
  source_stream_id TEXT NOT NULL REFERENCES source_streams(id) ON DELETE RESTRICT
) STRICT;

CREATE TABLE narrative_source_excerpt_segments (
  node_id TEXT NOT NULL REFERENCES narrative_source_excerpt_values(id) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 255),
  segment_id TEXT NOT NULL REFERENCES transcript_segments(id) ON DELETE RESTRICT,
  PRIMARY KEY (node_id, ordinal),
  UNIQUE (node_id, segment_id)
) STRICT;

CREATE TABLE narrative_source_excerpt_corrections (
  node_id TEXT NOT NULL REFERENCES narrative_source_excerpt_values(id) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 255),
  correction_id TEXT NOT NULL REFERENCES transcript_corrections(id) ON DELETE RESTRICT,
  correction_revision INTEGER NOT NULL CHECK (correction_revision >= 1),
  PRIMARY KEY (node_id, ordinal),
  UNIQUE (node_id, correction_id)
) STRICT;
