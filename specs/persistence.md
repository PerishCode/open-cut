# Product persistence and revision contract

Status: Business design baseline.

## Storage shape

The API-owned SQLite database combines three deliberate forms:

```text
append-only proposal and transaction journal
normalized current-state projections
transactional activity outbox
```

It is not pure event sourcing, a whole-Project JSON blob store, or a cache of
another truth source. Normal reads use current projections; the immutable
journal owns audit, idempotency, undo provenance, and explanation.

Conceptual table families include:

- projects and aggregate revisions;
- assets and SourceGrant references;
- narrative documents and nodes;
- sequences, tracks, clips, link groups, and captions;
- alignments and their target anchors;
- edit proposals, proposal operations, transactions, and inverse operations;
- request identities and converged outcomes;
- AgentRuns, AgentTurns, Approvals, MediaJobs, ResourceJobs, ExportJobs, and
  ProductResources;
- operational Asset state, immutable artifacts, and ExportArtifacts;
- scoped activity/outbox records.

Large media, models, leased frames, proxies, and exports remain product-owned
files referenced by rows, never SQLite blobs.

Canonical derived schemas evolve by forward migration rather than dual reads.
When source-proxy v2 made exact decoded audio sample count a RenderPlan input,
the next well-ordered migration removed v2 preview jobs/plans/artifacts and
rebuilt the closed RenderPlan v3 tables. Source media and committed creative
state remain intact; derived work is regenerated under the current producer.

Exact time, revision, cursor, digest, and ID encodings follow
`specs/wire-contract.md`. Revisions and cursors are positive indexed SQLite
INTEGER values capped at the signed 64-bit maximum and are always decimal
strings on JSON wires.

## Creative commit boundary

One creative apply or approval execution uses one SQLite transaction:

1. resolve the request-identity row and reject digest reuse;
2. verify authenticated actor, Project, AgentRun, and active AgentTurn generation;
3. load the immutable proposal and verify its digest and schema version;
4. validate exact entity and structural revision preconditions;
5. resolve proposal-assigned durable IDs for transaction-local references;
6. apply the closed operation union to normalized projections;
7. construct and persist exact inverse operations and affected entities;
8. advance leaf, structural aggregate, and Project revisions;
9. insert the EditTransaction and final request outcome;
10. append activity/outbox records;
11. consume any exact Approval;
12. commit once.

Any failure leaves no creative projection, transaction, approval-consumption, or
activity subset. Post-commit delivery failure is recovered through the request
identity and converged outcome.

## Proposal and request identity

The uniqueness scope for a mutating request is authenticated actor identity plus
request ID. The row stores normalized input digest, command schema, Project,
Run/Turn provenance, current lifecycle status, and resulting durable IDs.

Proposal creation and proposal application use separate request identities and
normalized input digests. A proposal-create identity converges on its immutable
proposal. An apply identity may move from `approval-pending` to committed or a
terminal application outcome, and a retry returns that current state. Reuse with
a different command or normalized digest is always invalid.

Proposal content and digest are immutable. Proposal content lifecycle is a
separate `open | applied | stale | cancelled` projection. Apply attempts are
separate `ProposalApplication` rows so Approval denial or expiry does not rewrite
the proposal or its request identity. At most one application is
`approval-pending`; creator consumption commits that exact application without
an Agent retry. A later application needs a new request identity. An exact
revision conflict marks the proposal stale because forward-only revisions cannot
make its preconditions true again.

## Transaction-local entity references

Create operations declare bounded unique local symbols. Entity references use a
closed union:

```text
EntityRef = { id: DurableID } | { local: LocalID }
```

The service validates the local reference graph, rejects missing or duplicate
symbols and cycles that cannot be normalized, then allocates opaque durable IDs
when the proposal is created. The immutable proposal stores the allocation map;
later apply, approval, and idempotent retry reuse it.

This permits one proposal to create a Sequence, Tracks, linked A/V Clips,
NarrativeNodes, and Alignments atomically without accepting Agent-selected
durable IDs. Results return the stable `local -> durable` map.

Alignment current state is normalized into one typed `alignments` row and one to
64 ordered `alignment_targets` rows. Target columns form a closed relational
union for Caption, Clip, or raw timeline anchors; JSON blobs and nullable hybrid
targets are forbidden. A target-set replacement validates one homogeneous family
and happens in the same transaction as its Alignment revision and status change.

## Revision topology

Creative state has three revision levels:

- `projectRevision` advances once for every committed creative transaction;
- `sequenceRevision` and `narrativeRevision` advance for any change in their
  respective aggregate subtree;
- `entityRevision` advances for each changed Asset, Track, Clip, Caption,
  NarrativeNode, Alignment, or other leaf/structural entity.

A leaf edit advances that leaf, its owning aggregate revision, and the Project
revision. Membership or ordering changes additionally advance the structural
parent such as Track or Section. One transaction advances each affected revision
at most once.

Preconditions express only facts the operation used:

- clip reframe requires the Clip revision;
- track insertion requires the Track and declared neighbor revisions;
- node move requires old/new parent and sibling-anchor revisions;
- sequence format change requires the Sequence aggregate revision;
- whole-document transforms require the Narrative aggregate revision.

`baseProjectRevision` records planning context but does not reject a commit by
itself. An unrelated transaction may advance Project or aggregate revision while
an edit still succeeds if every declared dependency remains valid. The result
returns all new revisions.

Preview and Export pin `sequenceRevision`. Entity-anchored Alignment exactness
pins its source node plus Clip/Caption target revisions; a raw timeline-range
pins Sequence revision. CLI conflicts identify only failed preconditions and
never auto-merge.

## Journal and projections

Journal operation and inverse payloads store canonical JSON, command schema
version, normalized digest, and stable operation order. They are immutable after
commit. Current-state rows are fully typed and indexed for bounded Project,
Narrative, Sequence, and entity reads.

Startup does not replay all transactions. Ordered migrations upgrade journal and
projection schemas together, then invariant checks validate revision heads,
foreign keys, active proposal mappings, and outbox cursors. Migration is
forward-only under the repository cold-start contract.

The forward migration from the former one-Caption projection rewrites both
projection rows and immutable proposal/transaction canonical payloads into the
unified target union, then recomputes every affected digest and audit reference.
The active runtime contains no legacy operation decoder or dual-write path.

An administrative repair cannot silently rewrite journal history. A projection
rebuild tool, if later introduced, must be explicit, versioned, verified against
revision/digest checkpoints, and outside normal startup.

## Activity outbox

The current state transition and its activity/outbox append share one SQLite
transaction. SSE, Contracts reconciliation, CLI wait, and UI ledgers consume
records after a scoped cursor.

Publishing may lag a committed row, but restart resumes from the outbox. A
consumer gap reconciles from a current snapshot plus cursor; in-memory EventBus
delivery is never a correctness input.

Installation-scoped ProductResource activity and Project-scoped creative/job
activity have separate cursor scopes. Explicit wait references project shared
resource progress into the affected Project ledger.

Projection granularity, event envelopes, snapshot-plus-cursor reads, SSE
delivery, and CLI wait reconciliation are normative in
`specs/read-activity.md`. Activity rows carry compact invalidation and receipt
references, not whole Project or entity bodies.

## Operational updates

Media progress, artifact selection, availability, AgentTurn attachment, and job
state use their own short SQLite transactions and activity records. They never
advance creative revisions.

An operational transaction may schedule a later creative proposal, but it cannot
directly change Narrative, Sequence, Alignment, or Asset creative registration.

## Undo

Undo reads the committed transaction's exact inverse operations and constructs a
new proposal against current entity revisions. If an affected entity changed
after the original transaction, undo returns a normal typed conflict; it does
not partially reverse or move the Project revision backward.

Successful undo journals another forward EditTransaction with its own inverse,
request identity, revisions, and activity.

The public `edit undo` command names one committed transaction and carries one
new request identity. Because its effect is the exact stored inverse rather than
caller-authored operations, the application may create the immutable undo
proposal and commit it in one atomic service boundary. It uses the original
transaction's post-state entity revisions as exact preconditions. A changed
affected entity produces a normal conflict; the service never selects a subset,
silently rebases, or moves a revision pointer backward. Undoing an undo is the
ordinary redo behavior and creates another forward transaction.

The first release retains journal and inverse facts for the entire non-purged
Project lifetime. Artifact availability and creator-approved purge project an
explicit undo capability as defined in `specs/project-lifecycle.md`; automatic
time-based history truncation is forbidden.

## Recovery harness

Crash injection covers every numbered creative-commit boundary, proposal ID
allocation, approval consumption, outbox publication, job artifact publication,
and export publication. After restart, exactly one of two states is valid:

- no commit and no externally visible partial effect; or
- one complete commit discoverable through its original request identity.

Harness invariants compare normalized projections, journal heads, revision
topology, request mappings, and activity cursors without treating raw database
inspection as product black-box acceptance.
