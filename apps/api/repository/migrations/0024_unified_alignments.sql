PRAGMA defer_foreign_keys = ON;

CREATE TABLE alignments (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  narrative_node_id TEXT NOT NULL REFERENCES narrative_leaf_nodes(id) ON DELETE RESTRICT,
  narrative_node_revision INTEGER NOT NULL CHECK (narrative_node_revision >= 1),
  sequence_id TEXT NOT NULL REFERENCES sequences(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision >= 1),
  status TEXT NOT NULL CHECK (status IN ('exact', 'stale', 'unbound')),
  last_transaction_id TEXT NOT NULL REFERENCES edit_transactions(id) DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE TABLE alignment_targets (
  alignment_id TEXT NOT NULL REFERENCES alignments(id) ON DELETE RESTRICT,
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

INSERT INTO alignments (
  id, project_id, narrative_node_id, narrative_node_revision, sequence_id,
  revision, status, last_transaction_id
)
SELECT id, project_id, narrative_node_id, narrative_node_revision, sequence_id,
       revision, status, last_transaction_id
FROM caption_alignments;

INSERT INTO alignment_targets (
  alignment_id, ordinal, kind, caption_id, entity_revision,
  local_start_value, local_start_scale, local_duration_value, local_duration_scale
)
SELECT id, 0, 'caption', caption_id, caption_revision,
       local_start_value, local_start_scale, local_duration_value, local_duration_scale
FROM caption_alignments;

DROP TABLE caption_alignments;

CREATE INDEX alignments_node
ON alignments(project_id, narrative_node_id, id);

CREATE INDEX alignment_targets_caption
ON alignment_targets(caption_id, alignment_id, ordinal)
WHERE caption_id IS NOT NULL;

CREATE INDEX alignment_targets_clip
ON alignment_targets(clip_id, alignment_id, ordinal)
WHERE clip_id IS NOT NULL;
