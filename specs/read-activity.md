# Product reads and activity reconciliation

Status: Business design baseline.

## Boundary

Reads expose bounded, revisioned product projections. They do not expose SQLite
rows, whole-project blobs, journal replay, storage paths, sidecar state, or an
unbounded object graph. CLI and UI consume the same application read ports with
different transport adapters.

Every snapshot response includes the activity scope and cursor observed in the
same SQLite read transaction as the projection. A client subscribes or waits
strictly after that cursor, closing the snapshot-to-stream race.

## Initial projections

The first release defines these read shapes:

- `ProjectSummaryPage`: bounded project cards for the installation, with
  lifecycle status and latest project revision;
- `ProjectOverview`: project identity, explicit format defaults, aggregate
  heads, main sequence/document identities, operational health summary, and
  current scoped activity cursor;
- `AssetPage` and `AssetDetail`: registered creative identity, safe display
  metadata, stream/fingerprint facts, availability, selected artifacts, and job
  references, never a source path or SourceGrant material;
- `TranscriptPage`: immutable recognition segments plus authored correction
  overlays for an exact asset/source-time window;
- `NarrativeSubtree`: one bounded subtree or sibling page, node revisions,
  stable anchors, and Alignment summaries;
- `SequenceIndex`: sequence format, aggregate revision, ordered track summaries,
  duration, and item counts;
- `SequenceWindow`: clips, captions, and Alignment overlays intersecting an
  exact timeline range on selected tracks, including enough predecessor and
  successor context for conflict-safe edits;
- `EntityDetail`: one kind-discriminated entity and its exact revision;
- `ProposalDetail`, `TransactionHistoryPage`, `RunDetail`, `JobDetail`,
  `ApprovalDetail`, and `ExportDetail`.

Large collections use stable cursor pagination with explicit limits. Offset
pagination and implicit "all" reads are forbidden. A page echoes normalized
filters and carries a continuation cursor whose meaning is local to that query,
not the activity cursor.

Sequence windows use exact RationalTime boundaries and deterministic ordering by
track order, timeline start, then entity ID. Narrative pages use durable sibling
anchors and ID tie-breaks, never hidden database row order.

## Activity envelope

One committed state transition may append one or more compact activity records:

```text
ActivityEvent
  schema
  eventId
  scope: { kind: project, projectId } | { kind: installation, installationId }
  cursor
  kind
  occurredAt
  actorSummary?
  projectRevision?
  aggregateRevisions?
  changedEntityRefs[]
  resourceRefs[]
  outcomeRef?
  summaryCode
```

`changedEntityRefs` contains kind, ID, and resulting revision or tombstone state;
it does not embed full entities or creative text. `resourceRefs` points to runs,
jobs, approvals, proposals, transactions, artifacts, or exports. The kind union
is versioned and closed. Unknown kinds make the consumer refetch the relevant
snapshot; they are never treated as successful no-ops.

Project activity and installation ProductResource activity have independent
strictly increasing CursorStrings. An installation event relevant to a waiting
Project causes an explicit project-scoped reference event, preserving one
ordered cursor per consumer scope.

## Snapshot and delivery semantics

The repository opens a read transaction, reads the requested projection and
current scope cursor, and returns both before closing it. Mutation projections
and their activity append already commit atomically under the persistence
contract.

SSE delivery is ordered per scope and at-least-once. `Last-Event-ID` or the
Contracts adapter cursor resumes strictly after a known cursor. Duplicate event
IDs/cursors are idempotent. A missing range, expired retention, unknown schema,
or impossible revision jump invalidates affected cached projections and triggers
a bounded snapshot refetch.

SSE is an internal UI optimization. Agent behavior remains:

```text
activity list --after <cursor> --limit <n>
activity wait --after <cursor> --timeout <bounded-duration>
```

Both return the same normalized event union and newest cursor. A wait timeout
returns a valid `waiting` envelope. No long-lived Agent-facing subscription is
introduced.

## Contracts reconciliation

Contracts owns runtime decoding, cache identity, event reduction, gap detection,
and refetch policy. Web components receive immutable projections and typed
status; they do not merge raw OpenAPI shapes or maintain private revision truth.

An event is only an invalidation/explanation fact unless its kind explicitly
defines a complete safe reducer. Any ambiguity refetches. Optimistic UI drafts
remain separate from committed Contracts projections.

## Retention

The first release retains Project creative activity for the Project lifetime and
installation resource activity while any durable Project/job reference can need
it. If later compaction is introduced, the API must publish an explicit minimum
available cursor and checkpoint; silent cursor reuse or renumbering is forbidden.

## Harness

- race a commit between snapshot and subscription and prove no state is missed;
- duplicate, delay, disconnect, and reorder transport frames while preserving
  scoped durable order after reconciliation;
- reject cross-project cursors and pagination cursors used as activity cursors;
- bound every list/window/subtree response and test deterministic continuation;
- prove event payloads contain no original path, SourceGrant, session, broker,
  private creative body, or whole entity graph;
- compare CLI list/wait and Contracts SSE results against the same repository
  scope and cursor.
