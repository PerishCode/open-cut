CREATE TABLE clip_link_groups (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  sequence_id TEXT NOT NULL REFERENCES sequences(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision >= 1),
  tombstoned INTEGER NOT NULL CHECK (tombstoned IN (0, 1)),
  last_transaction_id TEXT NOT NULL REFERENCES edit_transactions(id) DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX clip_link_groups_sequence
ON clip_link_groups(sequence_id, tombstoned, id);

CREATE TABLE clips (
  id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 36),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  sequence_id TEXT NOT NULL REFERENCES sequences(id) ON DELETE RESTRICT,
  track_id TEXT NOT NULL REFERENCES tracks(id) ON DELETE RESTRICT,
  asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE RESTRICT,
  source_stream_id TEXT NOT NULL REFERENCES source_streams(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision >= 1),
  source_start_value INTEGER NOT NULL,
  source_start_scale INTEGER NOT NULL CHECK (source_start_scale BETWEEN 1 AND 2147483647),
  source_duration_value INTEGER NOT NULL CHECK (source_duration_value > 0),
  source_duration_scale INTEGER NOT NULL CHECK (source_duration_scale BETWEEN 1 AND 2147483647),
  timeline_start_value INTEGER NOT NULL,
  timeline_start_scale INTEGER NOT NULL CHECK (timeline_start_scale BETWEEN 1 AND 2147483647),
  timeline_duration_value INTEGER NOT NULL CHECK (timeline_duration_value > 0),
  timeline_duration_scale INTEGER NOT NULL CHECK (timeline_duration_scale BETWEEN 1 AND 2147483647),
  timeline_start_order_key TEXT NOT NULL CHECK (length(timeline_start_order_key) = 48),
  timeline_end_order_key TEXT NOT NULL CHECK (length(timeline_end_order_key) = 48),
  enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
  link_group_id TEXT REFERENCES clip_link_groups(id) ON DELETE RESTRICT,
  tombstoned INTEGER NOT NULL CHECK (tombstoned IN (0, 1)),
  last_transaction_id TEXT NOT NULL REFERENCES edit_transactions(id) DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX clips_window
ON clips(sequence_id, track_id, tombstoned, timeline_start_order_key, timeline_end_order_key, id);

CREATE INDEX clips_link_group
ON clips(project_id, link_group_id, tombstoned, id);
