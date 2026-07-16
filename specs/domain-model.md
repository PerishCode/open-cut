# Product domain model

Status: Business design baseline.

## Aggregate boundary

`Project` is the atomic creative-state boundary:

```text
Project
|- Asset
|  `- MediaReference
|- NarrativeDocument
|  `- NarrativeNode
|- Sequence
|  |- Track
|  `- Clip
|- Alignment
|- EditProposal
`- EditTransaction

AssetMediaState / TranscriptArtifact / AnalysisArtifact
`- reference Asset and advance as operational projections

ProductResource / ResourceJob
`- installation-scoped operational state

AgentRun / AgentTurn / MediaJob / ExportJob / Approval
`- reference Project or Asset but own separate state machines
```

Large media bytes, models, proxies, frames, waveforms, and export bytes are not
stored in the aggregate or SQLite rows. SQLite stores their identities and
durable references. Operational lifecycle state remains outside the creative
Project aggregate.

## Identity and revisions

Every durable entity has an opaque stable ID. File paths, display names, array
positions, timecodes, and agent-generated prose never serve as identity.

Three independent orderings are required:

- `projectRevision` increments once per committed creative transaction.
- `entityRevision` increments for each creative entity touched by that transaction.
- `activitySequence` orders operational Asset projections plus AgentRun,
  AgentTurn, MediaJob, ResourceJob, Approval, and ExportJob activity within its
  declared project or installation scope.

Activity updates do not invalidate creative edits. An edit carries the
`baseProjectRevision` used for planning plus exact entity revision preconditions.
Only a failed entity precondition rejects the commit as a conflict.

Media availability, analysis progress, and selected compatible artifacts are
operational Asset projections ordered by `activitySequence`; they do not bump
Project or Asset entity revisions. Creator changes to Asset registration,
authorized source identity, import mode, or tombstone state are creative changes
and do use EditTransaction revisions.

## Asset

An `Asset` identifies imported source media independently of its current path.
Its creative entity contains source authorization identity, accepted media
fingerprint, import mode, and tombstone state. Availability, detected facts, and
immutable analysis artifact selection are reconciled operational projections.

Source authorization identity is an opaque SourceGrant ID. Platform path,
bookmark, and file-handle material live outside the creative aggregate under
`specs/source-access.md`.

The accepted fingerprint is a versioned complete SHA-256. Fast filesystem
observations can detect likely change but never authorize relink, replacement,
managed publication, or export.

Original transcript output is immutable. A creator correction is an authored
`TranscriptCorrection` entity; it never rewrites the recognition artifact or
invents replacement token timing. Correction and caption derivation semantics
are defined in `specs/transcript-caption.md`.

Removing an asset from creative state creates a tombstone. Physical media and
derived artifact reclamation are separate, explicit collection operations.

## NarrativeDocument

The first narrative document kind is `paper-edit`. It is an ordered tree with a
restricted node union:

- `section`: hierarchy with a title and canonical BCP-47 language;
- `source-excerpt`: exact source asset ranges and optional transcript references;
- `authored-text`: complete plain UTF-8 text with explicit `spoken | on-screen`
  purpose and canonical BCP-47 language;
- `visual-intent`: a complete plain UTF-8 description with explicit
  `b-roll | composition | replacement` purpose and canonical language;
- `note`: complete plain UTF-8 non-exported creator context with canonical
  language.

All five variants share one physical NarrativeNode identity, parent relation,
sibling order, revision, tombstone, and transaction provenance. Typed payload
tables do not create a second identity or ordering truth. SourceExcerpt evidence
is immutable; it may be moved or tombstoned but is never rewritten in place.

Insert and move operations use stable `afterNodeId` sibling anchors. Storage
owns compact `order_index` values, which are never Agent-facing editing
primitives. Bounded subtree cursors bind the exact document and parent revision;
continuing after either changes fails instead of mixing snapshots.

The public mutation grammar has typed insert/update operations for section,
authored text, visual intent, and note, plus immutable-evidence SourceExcerpt
insertion. Move and remove are generic NarrativeNode operations. The root cannot
move or be removed, and a non-empty section cannot be removed. Alignment accepts
every non-section variant and rejects section nodes.

The first slice does not define arbitrary embedded widgets or a general block
extension system.

The executable edit-kernel public operation union includes:

- typed insert and complete-value update of section, authored-text,
  visual-intent, and note nodes;
- generic move and tombstone removal of any allowed NarrativeNode;
- add, replace, and tombstone one TranscriptCorrection without changing its
  immutable TranscriptArtifact;
- insert one token-bounded `source-excerpt` NarrativeNode with an
  exact fingerprint/artifact/segment/correction-revision evidence closure;
- add, replace, and tombstone one Caption item, including its explicit canonical
  BCP-47 language;
- bind one non-section NarrativeNode to one or more Caption, Clip, or timeline
  targets, or explicitly mark that Alignment `stale` or `unbound`.

Create operations may use proposal-local symbols, including an Alignment that
references a NarrativeNode and Caption created in the same proposal. Proposal
normalization allocates all durable identities and stores the complete mapping
before apply. Asset, Clip, transcript, and the remaining initial operation
families extend this same union; they do not introduce another transaction path.

## Sequence

A project may contain multiple sequences, but the first slice creates one named
`main`. Sequence state is the executable truth for preview and export. It uses
exact rational time and contains typed tracks, clips, caption items, format, and
reframe state as defined by `specs/timeline.md`.

Narrative intent never becomes executable solely because a node exists. A
transaction must create or change sequence entities.

## Alignment

`Alignment` is a durable many-to-many relationship between one narrative node
and one or more stable Sequence targets:

```text
Alignment
  id
  narrativeNodeId
  narrativeNodeRevision
  sequenceId
  targets[]
  derivedTimelineRanges[]
  status: exact | stale | unbound
  transactionId

AlignmentTarget =
  clip-range { clipId, clipRevision, localRange }
  | caption-range { captionId, captionRevision, localRange }
  | timeline-range { range, sequenceRevision }
```

An Alignment contains between one and 64 targets. One Alignment is one semantic
correspondence and therefore all targets in it belong to the same target family;
Caption, Clip, and raw timeline targets cannot be mixed. Targets are stored and
hashed in canonical target-key order, and duplicate target anchors are invalid.
A linked A/V realization consequently uses one Alignment with two Clip targets,
not one Alignment per stream.

Clip and Caption local ranges are exact offsets within those entities; absolute
timeline ranges are derived from current positions. Moving an anchored item can
preserve `exact` when the same transaction updates the target revision and proves
the local semantic range unchanged. Trim, split, content change, or removal must
explicitly remap targets or mark them stale/unbound.

An entity-anchored alignment is `exact` only when the NarrativeNode and all
target revisions match. A raw timeline-range has no stable item identity and
therefore pins the whole Sequence revision. It is reserved for meaningful gaps,
beats, or ranges that cannot bind to an executable item.

When a change cannot prove semantic correspondence, the alignment becomes
`stale`; the system does not silently repair it. `unbound` preserves an explicit
narrative item with no current timeline realization. Stale and unbound states
retain their last typed target snapshot as inspectable evidence; status changes
do not erase the correspondence that was invalidated.

## EditTransaction

Agent creative writes first produce an immutable `EditProposal`:

```text
EditProposal
  id
  projectId
  agentRunId
  agentTurnId
  requestId
  intent
  baseProjectRevision
  preconditions
  normalizedOperations
  inversePreview
  affectedEntities
  impactClassification
  digest
  createdAt
```

Proposal content never changes. Its content lifecycle is `open | applied | stale
| cancelled`, and those statuses do not rewrite its digest. Each distinct apply
request creates or converges on a `ProposalApplication` with status
`approval-pending | committed | denied | expired | stale`. At most one
application may await Approval at a time. An Agent apply references the proposal
ID and digest rather than resubmitting a different operation body.

An open proposal has no wall-clock expiry. Approval may expire, but proposal,
allocation, digest, and explanation history remain durable until Project purge.
Concurrent apply requests converge on at most one committed EditTransaction. A
later request against an already-applied proposal returns that same transaction;
it never manufactures another creative commit.

Responsive creator UI edits may use a product port that creates an implicit
proposal and transaction in one atomic service call. The resulting transaction
still retains the normalized proposal content and provenance.

UI drafts, checkpointing, transaction budgets, and actor provenance follow
`specs/editing-interaction.md`. Background services cannot perform creative
writes through a hidden `system` actor.

All creator and agent mutations use the same transaction shape:

```text
EditTransaction
  id
  proposalId
  projectId
  actor: creator | agent
  intent
  baseProjectRevision
  preconditions: entityId -> entityRevision
  operations[]
  inverseOperations[]
  affectedEntities[]
  committedProjectRevision
  agentRunId?
  createdAt
```

One transaction may atomically change narrative, sequence, and alignment state.
It either commits completely or leaves no creative mutation.

Undo does not move a revision pointer backward. It commits a new transaction
containing the stored inverse operations and its own current preconditions. Redo
is another forward transaction.

Mutating commands require an idempotency identity. A retry returns the current
converged outcome of that same logical request and never creates a second
proposal, approval, job, or transaction.

The journal, current projections, transaction-local reference mapping, and
revision cascade are defined in `specs/persistence.md`.

## Operation families

The initial closed operation union contains:

- asset: register an authorized source, relink, change import mode, and
  tombstone;
- transcript correction: add, update, and tombstone authored corrections;
- narrative: insert, update, move, and tombstone nodes;
- sequence: create sequence; add, split, trim, move, and remove clips; set format
  and reframe; add, update, or remove captions;
- alignment: bind item targets or raw ranges, remap the complete target set with
  semantic-coverage proof, mark stale, and unbind;
- grouping: link two to 64 explicit live Clips or completely dissolve one
  LinkGroup.

Operation extensions require a schema change and harness coverage. Arbitrary
JSON patches, SQL fragments, filesystem operations, and model-generated code are
not valid creative operations.

## External state machines

`AgentRun`, `AgentTurn`, `MediaJob`, `SequencePreviewJob`, `ResourceJob`,
`ExportJob`, and `Approval` do
not share the creative transaction revision:

- an AgentRun records intent, phase, tool receipts, edit transaction IDs, and
  waiting reason, not full model context;
- an AgentTurn owns one generation-scoped Agent process attachment and write
  lease;
- a MediaJob produces one immutable artifact version or a terminal failure;
- a SequencePreviewJob pins one Sequence revision and RenderPlan and produces
  one immutable Viewer artifact or terminal failure;
- a ResourceJob produces one signed installation-scoped ProductResource;
- an ExportJob pins one sequence and entity revision;
- an Approval records proposed impact and authorization state.

Cancelling an AgentRun does not undo committed transactions or implicitly cancel
independent jobs. Each relationship is explicit.

Their transition, waiting, recovery, and approval semantics are defined in
`specs/durable-work.md`.

## Invariants

- No creative mutation exists outside a committed EditTransaction.
- No float represents source or timeline time.
- No export reads an unpinned moving sequence head.
- No preview or export bypasses the shared normalized RenderPlan semantics.
- No analysis job directly edits narrative or sequence state.
- No analysis or availability update advances creative revisions.
- No transcript correction mutates the original transcript artifact.
- No creative transaction is attributed to a background `system` actor.
- No entity is identified by file path or ordered-list index.
- No fast file observation substitutes for authoritative Asset content identity.
- No cross-model synchronization occurs without explicit operations.
- No physical deletion follows from a creative tombstone alone.
- No Agent-facing command bypasses revision preconditions or idempotency.
- No Agent apply can change the normalized content of its referenced proposal.
