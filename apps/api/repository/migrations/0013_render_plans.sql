CREATE TABLE render_plans (
  digest TEXT PRIMARY KEY NOT NULL CHECK (digest GLOB 'sha256:*' AND length(digest) = 71),
  schema_version TEXT NOT NULL CHECK (schema_version = 'open-cut/render-plan/v1'),
  compiler_version TEXT NOT NULL CHECK (compiler_version = 'sequence-render-plan-v1'),
  purpose TEXT NOT NULL CHECK (purpose = 'sequence-preview'),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  sequence_id TEXT NOT NULL REFERENCES sequences(id) ON DELETE RESTRICT,
  sequence_revision INTEGER NOT NULL CHECK (sequence_revision >= 1),
  observed_project_revision INTEGER NOT NULL CHECK (observed_project_revision >= 1),
  output_profile TEXT NOT NULL CHECK (output_profile = 'webm-vp9-opus-sequence-preview-v1'),
  canonical_json TEXT NOT NULL CHECK (json_valid(canonical_json)),
  created_at TEXT NOT NULL
) STRICT;

CREATE INDEX render_plans_sequence_revision
ON render_plans(sequence_id, sequence_revision, purpose, compiler_version, created_at, digest);

CREATE TABLE render_plan_inputs (
  plan_digest TEXT NOT NULL REFERENCES render_plans(digest) ON DELETE RESTRICT,
  ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
  artifact_id TEXT NOT NULL REFERENCES media_artifacts(id) ON DELETE RESTRICT,
  artifact_digest TEXT NOT NULL CHECK (artifact_digest GLOB 'sha256:*' AND length(artifact_digest) = 71),
  PRIMARY KEY (plan_digest, ordinal),
  UNIQUE (plan_digest, artifact_id)
) STRICT;

CREATE INDEX render_plan_inputs_artifact
ON render_plan_inputs(artifact_id, plan_digest);
