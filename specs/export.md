# Pinned Sequence export

Status: Accepted implementation contract.

## Purpose and entry boundary

Export turns one exact committed Sequence revision into a verified immutable
project-owned delivery artifact. It is durable operational work, never a
creative edit and never an alias for Creator preview.

The Agent discovers and invokes only these stable product CLI leaves:

```text
open-cut export start --sequence-revision <revision> --preset webm-vp9-opus-v1 --request-id <id>
open-cut export show --job-id <job-id>
open-cut export retry --job-id <job-id>
open-cut export cancel --job-id <job-id> --request-id <id>
```

All four leaves require AppState Project, Sequence, Run, and active Turn
context. Show remains read-only, but it is authorized through the current Turn
and may observe a durable job created by an earlier Turn of the same Run. An
Agent-started ExportJob is owned by its Run and outlives its initiating Turn;
Turn termination only removes transient command delivery authority. CLI exit,
disconnect, or bounded-wait timeout never cancels work.

The Creator Export button invokes a separate first-party Contracts/API use case
over the same ExportJob application service, scheduler, renderer, verifier, and
artifact model. It never fabricates an AgentRun, AgentTurn, CLI command, or
hidden Agent invocation. Durable export ownership is the closed union
`AgentRun | Creator`: a Creator-owned job is bound to the durable creative
actor, not the short-lived UI session that happened to authorize a call.
Session expiry prevents new calls but never cancels or orphans durable work.

No Agent command accepts an output path, overwrite flag, renderer choice,
codec argument, quality knob, resource path, or sidecar/runtime input. The
project-owned destination needs no per-export Approval. Creator Save As,
reveal, external overwrite, and destination picker behavior are separate
first-party UI/lifecycle capabilities.

## Closed lifecycle and lineage

`export start` is an idempotent durable operational mutation. Its request
identity converges on one root lineage keyed by Project, Sequence, exact
revision, normalized preset, compiler, renderer capability, and input profile.
It never starts another render after transport loss.

The public lifecycle is:

```text
accepted -> blocked | queued | running -> succeeded
accepted -> blocked | queued | running -> failed | cancelled
```

Show and replay of start resolve the current related-lineage tail. Only the
explicit retry leaf may advance that tail. Retry accepts a recoverable failed,
cancelled, or succeeded-with-invalid-artifact tail and creates a new WorkJob
and producer ExportArtifact identity. It never mutates, overwrites, or revives
the predecessor. Cancel is idempotent; if atomic successful publication wins
the race, success is final and cancel reports the converged succeeded result.

Recovery is a closed action union shared with other durable media work:
`retry-job | relink-source | acquire-resource | adopt-revision |
update-runtime | none`. Invalid preset/format or deterministic resource-limit
failures advertise `none`; changed/missing source uses `relink-source`; active
revision drift at start uses `adopt-revision`; missing qualified capability
uses `update-runtime`.

## Full-quality render input

Final export must not consume the lossy, 1920-long-edge
`webm-vp9-opus-source-v1` Viewer proxy. Export preparation creates or reuses an
exact-stream `matroska-ffv1-pcm-render-input-v1` artifact for every selected
input binding.

The render-input profile is a cache-evictable project media artifact with:

- the same explicit SourceStream selection union and shared source epoch used
  by source proxies;
- the admitted full oriented square-pixel source raster, without the Viewer
  1920 limit or upscaling;
- even dimensions and SDR Rec.709 `yuv420p`; unsupported odd/anamorphic output,
  HDR, Dolby Vision, or unknown color is a typed profile failure rather than a
  hidden conversion;
- lossless FFV1 video in Matroska, with one presented output frame per source
  presentation frame and a complete exact source-to-render-input PTS map;
- 48-kHz stereo signed-16 PCM audio after the pinned channel projection and
  resample policy, with exact decoded sample count;
- deterministic metadata removal, tool/capability identity, manifest, media
  digest, map digest, and same-build-target byte-stability evidence.

The source-to-render-input transform is a declared normalization boundary, not
creative state. Render-input bytes may be evicted when unleased and
reproducible, but their immutable artifact metadata and producer lineage remain.
Missing or changed referenced source never falls back to a Viewer proxy.

## RenderPlan and capability

Preview and export share one instruction compiler and semantic evaluator.
Export adds `RenderPurposeExport` plus the closed output profile
`webm-vp9-opus-v1`; purpose and fully expanded output policy are included in
the RenderPlan digest, so a preview plan digest can never name an export.

The ExportJob persists the same immutable render-intent snapshot and exact
producer prerequisites used by Sequence preview. First claim compiles only
from that snapshot and the selected immutable render-input artifacts. It never
re-reads the moving Project/Sequence projection, a Viewer session, current
default artifacts, or an existing SequencePreview artifact.

One `open-cut-render` binary and Go evaluator may implement both purposes, but
export is registered as an independent `sequence-export-renderer-v1`
capability with its own build identity, conformance profile, release evidence,
product-status availability, executor registration, and output verifier. No
new sidecar, helper executable, PATH entry, Agent command, or runtime-topology
node is introduced.

## First preset

`webm-vp9-opus-v1` is immutable and admits only:

- exact committed even canvas dimensions, with no resize or quality fallback;
- pixel aspect `1/1` and SDR Rec.709 Sequence color policy;
- constant frame rate equal to the Sequence rational frame rate;
- `yuv420p`, limited-range Rec.709, left chroma, opaque black background;
- VP9 profile 0, constant-quality CRF 24, `deadline=good`, `cpu-used=2`, one
  thread, disabled row/tile/frame parallelism and runtime CPU dispatch;
- Opus 48-kHz stereo, 192-kbit/s constant bitrate, 20-ms frames, compression
  level 10, fixed-point S16 input, no dither;
- `frame-zero-two-second-grid-v1`,
  `webm-bitexact-no-segmentuid-v1`, and
  `exact-sample-count-discard-padding-v1`;
- the same exact half-open video/audio grids, fixed-point compositor/mixer,
  caption closure, gaps, and ceil tail padding as preview.

Non-square pixel aspect, odd canvas dimensions, HDR, or a plan beyond the
declared execution/storage budgets fails with a typed diagnostic. The preset
never rounds, scales, drops an instruction, invokes a host codec, or silently
uses another preset.

## ExportArtifact and public projection

Successful work publishes one immutable ExportArtifact containing canonical
manifest plus `export.webm`. Its durable authority binds Project, Sequence and
revision, preset, RenderPlan digest, exact input artifact digests, renderer
version/target, verified media facts, byte size, and content SHA-256.

Stable CLI data exposes only:

- Project, Sequence, exact revision, preset, root/current job identity and
  closed job state;
- terminal diagnostic and recovery action when applicable;
- ExportArtifact identity after success;
- semantic/presentation duration, dimensions, frame rate/count, audio sample
  rate/count, codecs, pixel format, channel layout, byte size, content digest,
  and `passed` verification;
- activity cursor.

It omits canonical path, render-input IDs and paths, RenderPlan bytes/digest,
renderer/helper/tool paths, capability catalog, attempt directory, datadir,
sidecar, broker, and topology state.

Export artifacts are retained until explicit Creator deletion. They are never
silently capacity-evicted. First-party delivery uses a separate authenticated,
UI-session-bound, API-instance-bound, short-lived read lease available only to
Electron main. The renderer receives neither that lease nor an artifact URL.

Save As creates a bounded, one-shot DestinationGrant in Electron main after a
native picker gesture. The grant owns the absolute destination path; Web,
Contracts, API, Agent, SQLite, logs, and activity never receive that path.
Contracts may retain only an opaque grant token plus safe display name while an
explicit overwrite decision is pending. Electron consumes the grant by copying
the exact leased `export.webm` to a same-directory staged file, validating byte
count and SHA-256 while streaming, syncing it, atomically replacing the final
name, and syncing the parent where the platform permits. A pre-existing target
requires an explicit Creator overwrite gesture. Failure never truncates or
removes the previous destination.

After a verified publish, Electron main creates an opaque DeliveryReceipt bound
to the exact destination file identity and current UI session. It is a
process-local convenience authority with a non-sliding 30-minute TTL and a
maximum of 32 entries; the oldest entry is discarded at capacity. It may be
reused for non-destructive Reveal during its lifetime, but is cleared on UI
session replacement and process exit. Reveal rechecks that the destination is
the same regular file, revokes a changed or missing receipt, invokes the native
file-manager action in Electron main, and returns only the safe display name.
The receipt, destination path, bookmark, and platform registry details are
never persisted or sent to API, activity, Agent, or CLI. Reveal is not a
general filesystem capability and has no Agent command.

## Creator export history and retention

Creator export history is a project-scoped bounded read of logical lineages,
not a flat attempt log. One item is keyed by the immutable root ExportJob and
projects the current tail, attempt count, origin (`creator | agent`), artifact
availability, recovery action, and exact pinned Sequence revision/preset. A
retry updates the lineage tail instead of creating a second history card.

History is ordered newest-first by immutable root creation time with UUIDv7 root
identity as the tie-breaker. Its opaque cursor binds both values; because root order never changes, concurrent job
progress and retries do not create duplicates or skipped roots. The first page
defaults to 20 items and no page may exceed 50. Project activity invalidates the
current window through Contracts; the Web never polls. Creator may see all
project lineages but only the safe origin enum, never Agent actor, Run, or Turn
identity. Agent has no history/list command and continues to address only a
known Run-owned lineage through the stable CLI.

ExportArtifact bytes are retained indefinitely unless the Creator explicitly
deletes the exact current-tail artifact or the Project is purged. There is no
capacity-driven silent eviction. Explicit deletion is a Creator-only mutation
with request identity and a second UI gesture. It atomically changes the
artifact to the durable `deleted` tombstone, appends project activity, removes
the physical bytes, and preserves the ExportJob, retry lineage, facts, digest,
and audit history. `deleted` is not represented as `invalid` or `evicted`; its
recovery action is `retry-job`.

Deletion revokes future content leases. A delivery that already opened and
verified its file may finish; a platform which prevents moving an open file may
return an in-use conflict without changing durable state. Physical deletion
uses an API-datadir-owned staging directory. Cold start restores staged bytes
when the transaction did not commit and removes them when the durable tombstone
did commit. A new retry always creates a new job and artifact identity.

## Publication and recovery

The executor renders below an attempt root, validates the helper result and
complete output bytes, writes and syncs a staged artifact tree, atomically
renames it into the canonical export area, syncs the parent, and then commits
artifact, job, attempt, and activity in one SQLite transaction.

Cold start removes only structurally recognized abandoned attempts,
publications, and unbound canonical UUID roots. A DB-ready ExportArtifact whose
manifest/media is missing, linked, malformed, digest-invalid, or fact-invalid
is quarantined and atomically marked invalid with durable activity. Core API
readiness continues. The old identity is never restored or overwritten;
explicit retry creates a new job/artifact lineage.

## Harness

- prove recursive CLI help is the installed Agent's only discovery and entry;
- prove start idempotency, explicit retry lineage, cancel/publication race, Run
  ownership, and Turn-independent durable progress;
- prove every export input is the exact full-quality render-input profile and
  no Viewer proxy/preview artifact can satisfy a prerequisite;
- prove preview and export share instruction semantics while purpose/output
  policies produce distinct canonical plans;
- run the real qualified renderer twice and prove same-build-target output
  digest stability plus exact media facts;
- corrupt staged/canonical output and prove cold-start cleanup or invalidation
  without blocking core readiness;
- prove the stable CLI never receives a path and the installed actor can create
  a rough cut, inspect Sequence frames, start export, and observe verified
  success using only CLI commands.
- prove Creator start/show/retry/cancel uses the same state machine without an
  AgentRun and survives UI-session replacement;
- prove ExportArtifact delivery leases are unusable by renderer JavaScript,
  another UI session, another API instance, CLI transport, or direct loopback;
- prove DestinationGrant expiry/one-shot use, explicit overwrite, staged digest
  validation, atomic replacement, and preservation of an existing destination
  on every pre-publication failure.
- prove DeliveryReceipt session replacement, non-sliding expiry, bounded
  eviction, exact-file identity check, safe reusable Reveal, and zero path
  persistence or renderer disclosure;
- prove project history collapses retries into one root lineage, keeps stable
  newest-first cursor pagination, exposes only safe origin, and is invalidated
  by project activity rather than polling;
- prove explicit deletion records `deleted`, hides bytes from new delivery,
  preserves job/facts/digest/audit, replays only the same request identity, and
  cold-start reconciles both pre-commit and post-commit deletion staging.
