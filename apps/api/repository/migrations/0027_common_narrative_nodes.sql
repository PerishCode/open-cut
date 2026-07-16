PRAGMA defer_foreign_keys = ON;

CREATE TABLE narrative_documents_v5 (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision >= 1),
  kind TEXT NOT NULL CHECK (kind = 'paper-edit'),
  root_node_id TEXT NOT NULL CHECK (length(root_node_id) = 36),
  FOREIGN KEY (root_node_id) REFERENCES narrative_nodes_v5(id) DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE TABLE narrative_nodes_v5 (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  document_id TEXT NOT NULL REFERENCES narrative_documents_v5(id) ON DELETE RESTRICT,
  parent_id TEXT REFERENCES narrative_nodes_v5(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision >= 1),
  kind TEXT NOT NULL CHECK (
    kind IN ('section', 'authored-text', 'source-excerpt', 'visual-intent', 'note')
  ),
  order_index INTEGER NOT NULL CHECK (order_index >= 0),
  tombstoned INTEGER NOT NULL CHECK (tombstoned IN (0, 1)),
  last_transaction_id TEXT NOT NULL REFERENCES edit_transactions(id) DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE TABLE narrative_section_values_v5 (
  id TEXT PRIMARY KEY NOT NULL REFERENCES narrative_nodes_v5(id) ON DELETE RESTRICT,
  title TEXT NOT NULL CHECK (length(CAST(title AS BLOB)) <= 262144),
  language TEXT NOT NULL CHECK (
    length(CAST(language AS BLOB)) BETWEEN 2 AND 64 AND
    language NOT GLOB '*[^A-Za-z0-9-]*'
  )
) STRICT;

CREATE TABLE narrative_authored_text_values_v5 (
  id TEXT PRIMARY KEY NOT NULL REFERENCES narrative_nodes_v5(id) ON DELETE RESTRICT,
  purpose TEXT NOT NULL CHECK (purpose IN ('spoken', 'on-screen')),
  language TEXT NOT NULL CHECK (
    length(CAST(language AS BLOB)) BETWEEN 2 AND 64 AND
    language NOT GLOB '*[^A-Za-z0-9-]*'
  ),
  text TEXT NOT NULL CHECK (length(CAST(text AS BLOB)) BETWEEN 1 AND 262144)
) STRICT;

CREATE TABLE narrative_visual_intent_values_v5 (
  id TEXT PRIMARY KEY NOT NULL REFERENCES narrative_nodes_v5(id) ON DELETE RESTRICT,
  purpose TEXT NOT NULL CHECK (purpose IN ('b-roll', 'composition', 'replacement')),
  language TEXT NOT NULL CHECK (
    length(CAST(language AS BLOB)) BETWEEN 2 AND 64 AND
    language NOT GLOB '*[^A-Za-z0-9-]*'
  ),
  description TEXT NOT NULL CHECK (length(CAST(description AS BLOB)) BETWEEN 1 AND 262144)
) STRICT;

CREATE TABLE narrative_note_values_v5 (
  id TEXT PRIMARY KEY NOT NULL REFERENCES narrative_nodes_v5(id) ON DELETE RESTRICT,
  language TEXT NOT NULL CHECK (
    length(CAST(language AS BLOB)) BETWEEN 2 AND 64 AND
    language NOT GLOB '*[^A-Za-z0-9-]*'
  ),
  text TEXT NOT NULL CHECK (length(CAST(text AS BLOB)) BETWEEN 1 AND 262144)
) STRICT;

CREATE TABLE narrative_source_excerpt_values_v5 (
  id TEXT PRIMARY KEY NOT NULL REFERENCES narrative_nodes_v5(id) ON DELETE RESTRICT,
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

CREATE TABLE narrative_source_excerpt_segments_v5 (
  node_id TEXT NOT NULL REFERENCES narrative_source_excerpt_values_v5(id) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 255),
  segment_id TEXT NOT NULL REFERENCES transcript_segments(id) ON DELETE RESTRICT,
  PRIMARY KEY (node_id, ordinal),
  UNIQUE (node_id, segment_id)
) STRICT;

CREATE TABLE narrative_source_excerpt_corrections_v5 (
  node_id TEXT NOT NULL REFERENCES narrative_source_excerpt_values_v5(id) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 255),
  correction_id TEXT NOT NULL REFERENCES transcript_corrections(id) ON DELETE RESTRICT,
  correction_revision INTEGER NOT NULL CHECK (correction_revision >= 1),
  PRIMARY KEY (node_id, ordinal),
  UNIQUE (node_id, correction_id)
) STRICT;

CREATE TABLE alignments_v5 (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  narrative_node_id TEXT NOT NULL REFERENCES narrative_nodes_v5(id) ON DELETE RESTRICT,
  narrative_node_revision INTEGER NOT NULL CHECK (narrative_node_revision >= 1),
  sequence_id TEXT NOT NULL REFERENCES sequences(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision >= 1),
  status TEXT NOT NULL CHECK (status IN ('exact', 'stale', 'unbound')),
  last_transaction_id TEXT NOT NULL REFERENCES edit_transactions(id) DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE TABLE alignment_targets_v5 (
  alignment_id TEXT NOT NULL REFERENCES alignments_v5(id) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 63),
  kind TEXT NOT NULL CHECK (kind IN ('caption', 'clip', 'timeline')),
  caption_id TEXT REFERENCES captions(id) ON DELETE RESTRICT,
  clip_id TEXT REFERENCES clips(id) ON DELETE RESTRICT,
  entity_revision INTEGER CHECK (entity_revision IS NULL OR entity_revision >= 1),
  local_start_value INTEGER,
  local_start_scale INTEGER CHECK (local_start_scale IS NULL OR local_start_scale BETWEEN 1 AND 2147483647),
  local_duration_value INTEGER CHECK (local_duration_value IS NULL OR local_duration_value > 0),
  local_duration_scale INTEGER CHECK (local_duration_scale IS NULL OR local_duration_scale BETWEEN 1 AND 2147483647),
  timeline_start_value INTEGER,
  timeline_start_scale INTEGER CHECK (timeline_start_scale IS NULL OR timeline_start_scale BETWEEN 1 AND 2147483647),
  timeline_duration_value INTEGER CHECK (timeline_duration_value IS NULL OR timeline_duration_value > 0),
  timeline_duration_scale INTEGER CHECK (timeline_duration_scale IS NULL OR timeline_duration_scale BETWEEN 1 AND 2147483647),
  sequence_revision INTEGER CHECK (sequence_revision IS NULL OR sequence_revision >= 1),
  PRIMARY KEY (alignment_id, ordinal),
  CHECK (
    (kind = 'caption' AND caption_id IS NOT NULL AND clip_id IS NULL AND
      entity_revision IS NOT NULL AND local_start_value IS NOT NULL AND
      local_start_scale IS NOT NULL AND local_duration_value IS NOT NULL AND
      local_duration_scale IS NOT NULL AND timeline_start_value IS NULL AND
      timeline_start_scale IS NULL AND timeline_duration_value IS NULL AND
      timeline_duration_scale IS NULL AND sequence_revision IS NULL) OR
    (kind = 'clip' AND caption_id IS NULL AND clip_id IS NOT NULL AND
      entity_revision IS NOT NULL AND local_start_value IS NOT NULL AND
      local_start_scale IS NOT NULL AND local_duration_value IS NOT NULL AND
      local_duration_scale IS NOT NULL AND timeline_start_value IS NULL AND
      timeline_start_scale IS NULL AND timeline_duration_value IS NULL AND
      timeline_duration_scale IS NULL AND sequence_revision IS NULL) OR
    (kind = 'timeline' AND caption_id IS NULL AND clip_id IS NULL AND
      entity_revision IS NULL AND local_start_value IS NULL AND
      local_start_scale IS NULL AND local_duration_value IS NULL AND
      local_duration_scale IS NULL AND timeline_start_value IS NOT NULL AND
      timeline_start_scale IS NOT NULL AND timeline_duration_value IS NOT NULL AND
      timeline_duration_scale IS NOT NULL AND sequence_revision IS NOT NULL)
  )
) STRICT;

INSERT INTO narrative_documents_v5 SELECT * FROM narrative_documents;

INSERT INTO narrative_nodes_v5 (
  id, project_id, document_id, parent_id, revision, kind, order_index,
  tombstoned, last_transaction_id
)
SELECT n.id, n.project_id, n.document_id, n.parent_id, n.revision, 'section',
       row_number() OVER (
         PARTITION BY n.document_id, n.parent_id ORDER BY n.order_key, n.id
       ) - 1,
       0,
       (
         SELECT t.id FROM edit_transactions t
         WHERE t.project_id = n.project_id
         ORDER BY t.project_revision, t.id LIMIT 1
       )
FROM narrative_nodes n;

INSERT INTO narrative_nodes_v5 (
  id, project_id, document_id, parent_id, revision, kind, order_index,
  tombstoned, last_transaction_id
)
SELECT id, project_id, document_id, parent_id, revision, kind, order_index,
       tombstoned, last_transaction_id
FROM narrative_leaf_nodes;

INSERT INTO narrative_section_values_v5 (id, title, language)
SELECT id, title, 'und' FROM narrative_nodes;

INSERT INTO narrative_authored_text_values_v5 (id, purpose, language, text)
SELECT id, 'spoken', 'und', text FROM narrative_authored_text_values;

INSERT INTO narrative_source_excerpt_values_v5
SELECT * FROM narrative_source_excerpt_values;

INSERT INTO narrative_source_excerpt_segments_v5
SELECT * FROM narrative_source_excerpt_segments;

INSERT INTO narrative_source_excerpt_corrections_v5
SELECT * FROM narrative_source_excerpt_corrections;

INSERT INTO alignments_v5 SELECT * FROM alignments;
INSERT INTO alignment_targets_v5 SELECT * FROM alignment_targets;

DROP TABLE alignment_targets;
DROP TABLE alignments;
DROP TABLE narrative_source_excerpt_corrections;
DROP TABLE narrative_source_excerpt_segments;
DROP TABLE narrative_source_excerpt_values;
DROP TABLE narrative_authored_text_values;
DROP TABLE narrative_leaf_nodes;
DROP TABLE narrative_nodes;
DROP TABLE narrative_documents;

ALTER TABLE narrative_documents_v5 RENAME TO narrative_documents;
ALTER TABLE narrative_nodes_v5 RENAME TO narrative_nodes;
ALTER TABLE narrative_section_values_v5 RENAME TO narrative_section_values;
ALTER TABLE narrative_authored_text_values_v5 RENAME TO narrative_authored_text_values;
ALTER TABLE narrative_visual_intent_values_v5 RENAME TO narrative_visual_intent_values;
ALTER TABLE narrative_note_values_v5 RENAME TO narrative_note_values;
ALTER TABLE narrative_source_excerpt_values_v5 RENAME TO narrative_source_excerpt_values;
ALTER TABLE narrative_source_excerpt_segments_v5 RENAME TO narrative_source_excerpt_segments;
ALTER TABLE narrative_source_excerpt_corrections_v5 RENAME TO narrative_source_excerpt_corrections;
ALTER TABLE alignments_v5 RENAME TO alignments;
ALTER TABLE alignment_targets_v5 RENAME TO alignment_targets;

CREATE UNIQUE INDEX narrative_documents_project_kind
ON narrative_documents(project_id, kind);

CREATE INDEX narrative_nodes_page
ON narrative_nodes(document_id, parent_id, tombstoned, order_index, id);

CREATE INDEX alignments_node
ON alignments(project_id, narrative_node_id, id);

CREATE INDEX alignment_targets_caption
ON alignment_targets(caption_id, alignment_id, ordinal)
WHERE caption_id IS NOT NULL;

CREATE INDEX alignment_targets_clip
ON alignment_targets(clip_id, alignment_id, ordinal)
WHERE clip_id IS NOT NULL;
