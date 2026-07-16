# Project genesis, lifecycle, and retention

Status: Business design baseline.

## Genesis

Project creation is a first-party creator use case in the first release. The
Agent may list and inspect Projects but cannot create, archive, tombstone,
restore, or purge one.

One atomic genesis transaction creates:

- the Project identity, creator-visible name, lifecycle state, and explicit
  format defaults;
- one `paper-edit` NarrativeDocument with an empty root section;
- one `main` Sequence;
- one video track `V1`, one audio track `A1`, and one caption track `C1`;
- initial aggregate/entity revisions and a Project-created activity record.

The initial format is explicit persisted creative state, never a machine or
encoder default:

```text
canvas: 1920 x 1080
pixel aspect: 1 / 1
frame rate: 30 / 1
audio: 48,000 Hz, stereo
color: SDR Rec.709
```

Creator settings may seed different values only by including them in the
validated create request. Later settings changes do not rewrite an existing
Project. A failed genesis leaves no partial Project, document, sequence, tracks,
request outcome, or activity row. Idempotent retry returns the same Project and
genesis IDs.

## Lifecycle states

The lifecycle is:

```text
active <-> archived
active | archived -> tombstoned -> active | archived
tombstoned -> purged
```

`archived` hides a Project from default active lists and prevents new Agent
runs, background enrichment, and exports, while preserving reads and an
explicit creator restore. It is reversible and not deletion.

`tombstoned` removes the Project from ordinary workspace lists and rejects new
creative writes/jobs. It retains the complete journal, projections, grants,
managed media, artifacts, and receipts for explicit restore. Tombstoning is a
durable first-party action with an impact preview; it never means filesystem
deletion.

`purged` is terminal. It requires exact durable Approval and an impact manifest
pinning the Project lifecycle revision and every irreproducible byte or undo
capability that will be lost. Purge destroys product-managed source bytes,
SourceGrant references, derived artifacts, exports, and private creator content
according to platform policy, then retains only the minimum non-content audit
tombstone needed to prevent identity/request reuse. Referenced original host
files are never deleted.

## Creative journal and undo horizon

The immutable proposal/transaction journal and current creative projections are
retained for the full non-purged Project lifetime. The first release has no
time-based journal compaction, history truncation, or destructive automatic GC.

Every committed transaction projects an `UndoCapability`:

```text
ready
needs-regeneration
source-missing
conflict
purged
```

`ready` means all inverse facts and required bytes are available, not that
current entity preconditions are guaranteed to pass. `needs-regeneration`
identifies reproducible derived artifacts that may be recreated before inverse
apply. `source-missing` preserves the inverse proposal but reports unavailable
referenced media. `conflict` is computed against current revisions when undo is
requested. `purged` is terminal and names the approved purge receipt.

Undo remains a new forward transaction and never moves revision heads backward.

## Media retention

- Referenced originals remain creator-owned and are never collected or deleted
  by Open Cut.
- SourceGrant protected data remains while a non-purged Asset needs it, unless
  the creator explicitly relinks or revokes access; loss degrades availability
  without deleting creative identity.
- Managed originals and irreproducible artifacts are retained through archive
  and tombstone. Deletion requires the Project purge or a separately approved
  protected action whose impact enumerates affected transactions/exports.
- Reproducible proxies, thumbnails, waveforms, and temporary frames may use
  bounded LRU collection when not leased or pinned by a job/export. Acquired
  default transcription models are declared offline retention in the first
  release and require explicit Creator removal. Their rows keep enough
  provenance to regenerate or reacquire them.
- Attempt temporary files are scheduler recovery material, not Project history,
  and follow verified attempt cleanup rules.

There is no hidden "clean storage" action that changes creative state or erases
undo evidence. Before any protected collection, the UI receives the exact
affected Project, Asset, transaction, export, offline, and regeneration impact.

## Identity and naming

Project IDs are service-generated durable identities. Names are creator text,
need not be unique, and never become a filesystem path or authorization scope.
Project-owned artifacts derive storage from the ID through repository adapters,
not from the name.

Genesis IDs for the document, sequence, and tracks are server-allocated and
returned in the create result. No client relies on labels such as `V1` as durable
identity.

## Harness

- crash at each genesis write and prove all-or-nothing idempotent creation;
- create with defaults and explicit overrides and prove later settings do not
  mutate either Project;
- enforce lifecycle write/job policy and reversible archive/tombstone restore;
- prove referenced host files survive every lifecycle transition including
  purge;
- enumerate undo capability changes across artifact eviction, source loss,
  relink, conflicting edits, managed-source deletion, and purge;
- reject purge without exact current impact approval and prove late/stale
  approvals cannot delete bytes;
- prove Project names never enter canonical storage paths.
