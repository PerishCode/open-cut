# Sequence frame inspection

Status: Implemented vertical-slice contract.

## Purpose and boundary

`open-cut sequence frames` lets an Agent inspect the exact committed Sequence
composition it is editing. It is a turn-scoped operational read: it may create
derived work and temporary read-only resources, but it never changes creative
state or Project/Sequence revisions.

The stable product CLI and its recursive `--help` documents are the Agent's only
entry. The Agent never receives an API, Viewer session, sidecar, repository,
preview artifact path, renderer path, or work-scheduler entry.

## Closed invocation union

One leaf exposes three mutually exclusive operations:

```text
open-cut sequence frames --sequence-revision <revision> --time <value/scale>...
open-cut sequence frames --job-id <job-id>
open-cut sequence frames --retry-job-id <job-id>
```

Prepare accepts one to eight strictly increasing exact Sequence times. Continue
observes one exact frame-job lineage. Retry is explicit and accepts only a
recoverable failed/cancelled tail; it never hides a retry behind an identical
prepare call. All operations require AppState Project, Sequence, Run, and active
Turn context.

## Exact preview prerequisite

Prepare internally ensures the exact SequencePreview job for the requested
Sequence revision using the API's authenticated renderer/resource closure. This
is an application-layer dependency, not another Agent surface and not a Creator
Viewer continuation.

The new `sequence-frame-set` WorkJob initially pins Project, Sequence, Sequence
revision, preview job, requested times, frame-grid policy, and output profile.
When that exact preview job succeeds, reconciliation atomically binds its ready
artifact ID, content digest, render-plan digest, and media facts before the frame
job becomes claimable. The frame job never follows a preview retry, replacement
artifact, current Viewer session, or moving Sequence head.

If a prerequisite fails, the frame job reaches a typed terminal failure. An
explicit retry creates a related preview/frame chain only when the terminal
failure is classified recoverable; otherwise the original terminal result is
returned unchanged.

The WorkJob is durably owned by its Run. The active Turn is delivery authority,
not job lifetime: ending the Turn revokes scratch resources but does not erase
the canonical artifact or rewrite WorkJob state. Retry never mutates or
resurrects the predecessor. It creates a new frame job and producer artifact
lineage; if the exact preview tail itself has a recoverable failure, retry first
creates the explicit related preview retry. Deterministic invalid input remains
terminal, artifact/decode/attempt failures use `retry-job`, missing input uses
`relink-source`, revision drift uses `adopt-revision`, and an unknown executor
failure uses `update-runtime`.

## Time and sampling semantics

For exact Sequence frame rate `N/D` and requested time `t`, sampling uses:

```text
frameIndex       floor(t * N / D)
sequenceGridTime frameIndex * D / N
```

Every request must satisfy `0 <= t < exact preview presentation duration`.
There is no nearest-frame selection, clamp, invented tail frame, Viewer
playhead, locale, or UI-selection input. Results return both requested time and
the selected Sequence grid time.

The executor streams the exact pinned SequencePreview media through the
authenticated contained decoder; no canonical artifact path is passed to the
child. It selects the exact decoded ordinal, never a timestamp approximation.
Output never upscales; when required it scales bilinearly to a 1280-pixel maximum
long edge while preserving aspect ratio with rational half-up dimension
rounding. Go then emits metadata-free, fully opaque sRGB PNG bytes. Decoder
closure, grid policy, and PNG profile are part of producer authority. One typed immutable
`sequence-frame-set` artifact owns its manifest and sample bytes. It is not an
Asset `frame-sample-set` artifact because its coordinate system and provenance
are Sequence time and a rendered composition.

## Turn scratch resources

A ready result materializes one read-only PNG per sample under a separate
active-Turn scratch lease. Leases last exactly five minutes and include resource
ID, MIME type, byte size, SHA-256, requested time, Sequence grid time, and
expiry. The public path is a capability result for a generic image reader; it is
not a durable artifact reference.

The samples are one all-or-nothing lease set rooted at
`<datadir>/scratch/runs/<run>/turns/<turn>/<lease-set>/<resource>.png`. Polling
the same ready job during the live interval reuses every resource ID, path, and
expiry exactly; it never slides the TTL. After expiry, a new whole set is
materialized from the canonical artifact. Partial rows or partial files are
never observable.

Sequence frame leases use their own storage projection. They never reuse or
extend Creator Viewer leases, never alter Viewer generation/playhead/pinned
revision, and are removed when expired or when their owning Turn ceases to be
active.

## Idempotency and observable state

The prepare logical key includes the exact preview job, normalized requested
times, policy, and profile. Repeating it observes the same blocked, queued,
running, succeeded, failed, or cancelled job. A succeeded job whose immutable
artifact is no longer ready is not treated as ready; rematerialization requires
an explicit new lineage.

The result always identifies Project, Sequence, exact Sequence revision, job,
profile, and activity cursor. `accepted | ready | failed` is closed. A failed
result includes the stable terminal error code already owned by WorkJob state.
Operational activity may advance the activity cursor but never the creative
revision.

Stable CLI data deliberately omits preview job/artifact identity, canonical
frame artifact identity, render-plan digest, executor identity, publication
paths, and recovery work paths. Those remain internal provenance. Only durable
request, completion, failure, eviction, and retry facts enter activity; lease
polling and renewal noise do not.

## Publication and cold-start recovery

Publication writes a complete staged manifest/PNG tree, syncs and atomically
renames it, then commits the ready artifact binding in SQLite. Cold-start
reconciliation understands only structurally valid UUID attempt/publication
roots. It removes abandoned staged work and recognized unbound canonical
orphans. A DB-ready artifact whose manifest or PNG bytes are missing, corrupt,
non-opaque, dimensionally invalid, or digest-invalid is quarantined and marked
`evicted` in an atomic recovery transaction with durable activity. Core API
readiness continues; subsequent continuation returns a typed recoverable
terminal delivery result and requires explicit retry.

## Harness

- prove arbitrary requested times floor to the exact Sequence frame grid;
- reject duplicate, unordered, negative, tail-equal, and out-of-range times;
- prove the frame job binds one exact preview job/artifact/digest and never
  follows Viewer state or a retry tail;
- prove identical prepare is observation while explicit retry creates lineage;
- decode a committed composition and verify PNG size/digest plus requested/grid
  times through the stable CLI;
- prove resources exist only in the active Turn scratch root, reuse as an exact
  non-sliding whole set, and expire without changing Project/Sequence state;
- corrupt a canonical PNG, restart recovery, prove eviction/activity, and prove
  explicit retry creates new job and producer-artifact lineage;
- execute ordinals zero and one against the authenticated real decoder and prove
  distinct, bounded, opaque PNG results;
- prove no HTTP, sidecar, renderer, artifact, database, or Viewer authority is
  present in the installed actor environment or evidence bundle.
