# Local media pipeline

Status: Business design baseline.

Viewer delivery of these artifacts uses the opaque, session-bound transport in
`specs/media-delivery.md`; this pipeline never turns a canonical artifact or
SourceGrant path into a Web-facing URL.

## Scope

The media pipeline imports local source references, detects stable facts, creates
immutable derived artifacts, and reports durable progress. It does not decide
creative structure or mutate narrative and sequence state.

## Pinned media toolchain

The API sidecar artifact closure ships one platform-specific, version-pinned
media toolchain for probing, frame decode, waveform extraction, proxy
generation, preview-compatible transforms, and export. The tools and their
`media-tools.json` manifest live beside the API executable under
`dist/sidecar`; the API resolves them only relative to its own physical
executable directory. A tool path never comes from the data directory, `PATH`,
the current working directory, settings, environment, UI, Agent input, or a
runtime-topology product extension.

This ownership is deliberate: media tooling is an API implementation
dependency, not a launcher, runner, sidecar-protocol, or installation-data
concept. Development, packaged execution, and harnesses consume the same
artifact-relative manifest. Repository code never resolves a system-installed
decoder or encoder as a product fallback.

The first distributable FFmpeg family is an explicit LGPL-only build. Its
configuration contains `--disable-gpl`, `--disable-nonfree`, and
`--disable-version3`; GPL or nonfree components and dependencies, including
libx264 and libx265, are absent. Release compliance still requires legal review
for target markets and every enabled dependency.

The manifest binds its schema, public target, toolchain and source versions,
source and build-recipe digests, exact configuration, license profile, notices,
and every executable's relative path, byte size, SHA-256, and closed capability
set. The complete manifest is validated before any capability is registered.
One missing, escaping, linked, non-regular, non-executable, or digest-mismatched
file rejects that toolchain atomically; API readiness remains independent and
its jobs remain queued.

Release construction starts from an authenticated, digest-pinned source
archive and records the exact source, patches, compiler/build environment,
configuration, SBOM, license notices, and corresponding-source artifact. It
never downloads a third-party executable. Tool executable identity and digest
are verified as payload contents. Every artifact and ExportJob records the
relevant toolchain version and normalized parameters. Hardware acceleration may
vary by platform, but a hardware path cannot change timeline semantics,
timestamps, crop, caption, or input selection. Unsupported hardware falls back
within the same pinned toolchain.

The first internal Viewer proxy profile is seekable WebM with VP9 video and
Opus audio. It is a reproducible internal artifact, not an export promise. Its
versioned profile fixes keyframe cadence, pixel/color policy, audio projection,
and an exact source-to-proxy time map. H.264/AAC, HEVC, and other distribution
presets require separate codec and target-market approval and cannot silently
replace this profile.

The closed source-preview profile is `webm-vp9-opus-source-v1`. It is distinct
from the similarly encoded Sequence/export preset because a source proxy has no
committed Sequence frame grid. It preserves one presented video frame per
source presentation frame, including variable frame rate, and rebases media
timestamps without inventing creative coordinates. Its canonical manifest
records every output video PTS paired with the exact input source PTS. Sequence
preview and export normalize only after a RenderPlan pins the Sequence rational
frame rate.

All selected tracks share one rebase epoch: the earlier of the first presented
selected-video timestamp and the first valid selected-audio timestamp. Output
time zero denotes that exact source time. A track that begins later retains its
leading gap; video and audio are never independently reset to zero. Negative
source timestamps are valid inputs to this calculation. The manifest records
the common epoch plus each selected track's exact source and proxy start time,
so decoding, seeking, and later RenderPlan substitution preserve source A/V
alignment.

That rebase never changes the product source coordinate. Clip, Transcript and
Source Viewer ranges use original absolute presentation time. A finite stream
with descriptor start `s` and duration `d` covers `[s, s+d)`; validating it as
`[0,d)` is invalid whenever `s != 0`. Proxy time is derived from the manifest's
source epoch only at the playback/material adapter.

For the automatic registration job, stream selection is a closed operational
policy rather than an implicit creative choice. Video and audio are selected
independently: choose the lowest container index carrying the `default`
disposition, otherwise the lowest index of that media type. At least one must
exist. The selected durable SourceStream IDs and descriptors are expanded into
the artifact manifest and producer digest. Any Clip or explicit analysis request
still names its SourceStream IDs; it never inherits this operational default.

Source-proxy job parameters use one closed selection union:

```text
{ policy: default-v1 }
| { policy: explicit-v1, videoStreamId?, audioStreamId? }
```

The explicit form requires at least one stream and includes every selected ID
in normalized parameters and the logical job key. `default-v1` exists only for
registration prewarming. Render preparation resolves every Clip's committed
SourceStream ID and may reuse a ready proxy only when its immutable manifest
contains that exact stream. Otherwise it schedules an explicit proxy variant;
it never falls back to the registration default. Combining exact video and
audio streams in one proxy is a deterministic storage/execution optimization,
not LinkGroup or creative render semantics.

The profile uses pinned software libvpx VP9 and fixed-point libopus inside the API artifact
closure, maximum 1920-pixel long edge without upscaling, even square-pixel
dimensions, `yuv420p`, SDR Rec.709, and 48 kHz stereo Opus with a fixed channel
projection. The first presented video frame is a keyframe; thereafter the first
presented frame on or after each two-second proxy-time boundary is forced to be
a keyframe. A sparse VFR source does not gain invented frames merely to satisfy
a wall-clock keyframe interval. A video-only or audio-only source produces the
corresponding valid WebM track set. Encoder, resampler, filter, muxer,
rate-control, threading, color, channel, and metadata settings are all
explicit; ambient FFmpeg defaults are not part of the profile.

The initial source-proxy encoder fixes the same target CPU baseline as the
Sequence renderer and disables libvpx runtime CPU detection. ARM64 uses
baseline NEON without dot-product, I8MM, SVE, or SVE2; x64 disables optional
SSE3/SSSE3/SSE4.1/AVX-family dispatch. A machine with newer instructions cannot
silently produce a different artifact under the same producer identity.
libopus is built with its fixed-point core and with assembly, runtime CPU
detection, and intrinsics disabled. Its float API remains linkable for the
pinned FFmpeg wrapper, but renderer-visible proxy decode explicitly selects
`libopus` and requests packed signed-16 output; the native FFmpeg Opus decoder
and floating-point sample path are not interchangeable product semantics.

`v1` never obtains SDR output by merely retagging HDR bytes. Clearly tagged PQ,
HLG, Dolby Vision, or other unsupported HDR input fails the job with a typed
color-profile error until a pinned tone-mapping dependency and policy is part
of a later profile. Supported SDR input is converted explicitly to Rec.709;
missing color tags use the profile's recorded Rec.709 assumption rather than an
FFmpeg ambient heuristic. Audio layout follows a versioned downmix table for
recognized layouts; an unknown layout fails instead of inheriting an ambient
mix matrix. These restrictions affect only the proxy job, not Asset
availability or creative state.

libvpx and libopus are authenticated source inputs to the same media closure,
not machine prerequisites. Their versions, archive URLs and SHA-256 values,
static build recipes, licenses, notices, and final linkage are manifest/SBOM
facts. A missing or mismatched dependency withholds the proxy capability while
API readiness and non-proxy work remain available.

The immutable v2 source-proxy artifact directory has exactly `manifest.json`,
`proxy.webm`, and, when video exists, `video-time-map.bin`. The canonical
manifest transitively binds both files' SHA-256 and byte length, the selected
SourceStream IDs and full descriptors, common epoch, exact track starts, output
facts, producer/toolchain identity, and all normalized profile parameters. The
time map begins with the eight ASCII bytes `OCPMAP01`, then an unsigned 64-bit
big-endian record count, followed by that many ordered pairs of signed 64-bit
big-endian `(sourcePTS, proxyPTS)`. Source and proxy time bases live in the
manifest. Pairing is by presentation ordinal, both PTS sequences are strictly
increasing, and the count must equal the probed proxy video-frame count. The
binary map remains compact for long footage while its digest makes it one
immutable semantic unit with the human-inspectable manifest.

For an audio track the manifest also records `decodedSampleCount`: the exact
number of stereo S16 samples visible after Opus pre-skip and end discard. The
producer obtains it by a bounded full frame inventory that explicitly selects
the pinned libopus decoder and S16 format. Container duration, packet duration,
and the FFmpeg native Opus decoder cannot substitute for this value. The count
is positive and bounded by the source-proxy audio admission limit.

## Import modes

The default import mode is `referenced`: Open Cut records a source reference and
fingerprint while leaving original bytes in place.

That reference is an opaque SourceGrant governed by `specs/source-access.md`.
Media/domain code never uses a creator path as identity or Agent-visible state.

An explicit `managed` mode copies source bytes into API-owned product data.
Changing import mode is a durable collection operation, not an implicit side
effect of opening or editing a project.

Every asset has a product-generated ID independent of path. Registration records
a fast observation such as filesystem identity, byte size, and modification
time, then computes a complete SHA-256 content fingerprint asynchronously. The
observation detects likely changes but is never content identity.

While hashing, the Asset is `identifying`. The first scheduler keeps probe
blocked on `fingerprint-required`; after identification it runs probe, then
unblocks proxy, waveform, and transcript work on `facts-required`.
Transcript additionally remains blocked on `model-required`. A later executor
may precompute into an unpublished attempt area in parallel, but no derived
output becomes selected until the complete strong fingerprint and required
facts are committed. Automatic relink acceptance, managed-copy publication, and
RenderPlan compilation require that fingerprint.

A source path may be updated by a creator relink operation without changing
asset identity only when the complete versioned SHA-256 matches. A mismatch
requires either a new Asset or an explicit creator replacement transaction; it
never silently changes the accepted identity.

Register, relink, import-mode, and tombstone changes are revisioned Asset
operations inside an EditTransaction. Availability, detected facts, job
progress, and compatible artifact selection are operational projections ordered
by product activity and do not advance creative revisions.

The first referenced registration commits the Asset, creator Proposal,
EditTransaction, request identity, Project/installation activity, five logical
jobs, and their complete typed prerequisites in one SQLite transaction. The
scheduler reconciles that set against the active-payload executor registry
before recovery completes and again before every claim. This queues `identify`
under the mandatory self executor, keeps `probe` blocked on fingerprint, keeps
proxy and waveform blocked on facts plus their executor capability, and keeps
transcript blocked on facts, model, and executor capability. Successful identification atomically selects the initial
fingerprint, marks the Asset online, succeeds its attempt and logical job,
appends operational activity, and reconciles probe. Successful probe then
reconciles the dependent jobs. Neither transition advances Project revision.

Registration never creates a parameter-incomplete frame placeholder. A
frame-sample-set job exists only after `asset frames` supplies its explicit
SourceStream and exact ordered request times; those immutable normalized
parameters are present from job creation onward.

The five registration jobs use closed semantic profiles rather than a shared
`initial` placeholder: full SHA-256 identity, ffprobe facts, the automatic
source-preview proxy above, a fixed waveform profile, and the pinned
multilingual transcript profile. Proxy/waveform/transcript stream selection is
deterministically expanded from fingerprint-pinned facts after probe; changing
that selection policy requires a new profile and logical job identity.

An executor kind is claimable only when its exact implementation is registered
by the active payload. Therefore probe/decode jobs remain blocked on
`executor-required` while a development payload lacks the pinned media
toolchain; the scheduler never substitutes a system decoder.

Asset inspection is a bounded snapshot of the most recent 32 jobs and 32
artifacts; reaching that bound never makes the Asset unreadable. Exact command
polling resolves its logical job directly, while older operational history
remains available through durable activity and dedicated history reads.

## Availability

Asset availability is explicit:

- `identifying`: registered bytes are still receiving authoritative identity;
- `online`: source bytes match the recorded identity policy;
- `changed`: a source exists but no longer matches;
- `missing`: the reference cannot be resolved;
- `managed`: product-owned source bytes are present;
- `unreadable`: source resolution exists but access fails.

Missing or changed originals do not delete creative state. Narrative excerpts,
clips, transcript artifacts, and alignments remain addressable and surface their
degraded availability.

The local agent never receives arbitrary source paths. CLI results use asset IDs,
display names, bounded media facts, and content resources.

## Media facts

Detected facts are versioned metadata, not filename inference:

- container and stream inventory;
- exact duration and source time base;
- video dimensions, display orientation, pixel aspect, and frame rate;
- audio channel layout and sample rate;
- rotation and color metadata needed for faithful preview;
- byte size and media fingerprint.

Each source stream receives a product-generated `sourceStreamId` tied to the
Asset, accepted fingerprint, media type, and stable container-stream identity.
The same bytes and container stream preserve that ID across probe attempts and
producer upgrades even when non-identity metadata becomes richer. Container
indexes remain facts, not Clip identity. A detected primary stream is a
UI/Agent suggestion only; normalized edit operations commit the exact selected
stream IDs.

For the first closed probe adapter, exact source bytes are pinned by fingerprint
and the stable container-stream key is `(assetId, fingerprint, containerIndex)`.
Reprobing may update that row's descriptor digest and metadata but cannot replace
its `sourceStreamId`. The index is therefore an adapter key within immutable
container bytes, never a creative Clip identity or a substitute for the durable
stream ID.

All time is exact rational time. Floating-point seconds may appear only as a
display projection.

## Derived artifacts

Derived bytes live below the API-owned app data directory and are addressed by
logical artifact identity rather than exposed paths. Initial artifact kinds are:

- proxy media;
- waveform summary;
- thumbnail and bounded frame sample sets;
- transcript;
- shot or scene boundaries;
- lightweight semantic index.

An artifact records asset ID, kind, producer version, input fingerprint,
parameters, lifecycle state, byte reference, and creation time. Successful
artifact contents are immutable. Re-analysis creates another version and
atomically selects the active compatible artifact.

Cache eviction may remove reproducible bytes but cannot erase artifact history
needed to explain creative results. A missing reproducible byte is regenerated
through a MediaJob.

## Transcript

A transcript artifact contains ordered segments with:

```text
segmentId
assetId
sourceRange
text
speaker?
confidence?
language?
```

Segment IDs are artifact-local stable identities. Source ranges remain exact
even when a creator supplies an authored text override.

Original transcript output is immutable. Corrected wording, paper-edit prose,
captions, and voice-over text belong to authored project state.

Transcription executes locally. The first engine is a pinned source-built
`whisper.cpp` executable inside the API sidecar artifact closure. The initial
profile uses the multilingual Whisper `small` model, automatic language
detection, original-language transcription, and no translation or speaker
diarization. Engine version, target backend profile, and model identity are part
of producer identity. Recognition models are signed, versioned
installation-scoped ProductResources
acquired on demand into API-owned product data after creator UI authorization.
The Agent never downloads a model, selects a host path, or receives model bytes.

The active signed release's authenticated resource catalog fixes compatible
model identity, origin, byte size, and SHA-256. The API does not accept an Agent,
UI, environment, or settings override for those supply-chain fields.

The sidecar registers the transcript executor only when the verified unified
media catalog contains `local-transcription-v1` and the adjacent authenticated
resource catalog contains the exact compatible multilingual-small entry. The
engine, FFmpeg, FFprobe, recipe, target, conformance evidence, and test-model
resource closure form the executor version; the production model entry/content
digests form the immutable job binding. Neither registry becomes an Agent or
runtime-topology surface.

A transcript artifact records engine version, model identity and digest,
language parameters, and input audio fingerprint. Changing any of them produces
a new immutable artifact.

If a compatible model is missing, the Transcript MediaJob enters `blocked` with
typed reason `model-required` and a referenced ResourceJob or resource
requirement. Other analysis and manual editing remain available. After the
resource becomes ready, waiting compatible jobs return to `queued` and continue
without requiring the original Agent process. Once acquired, transcription is
fully offline.

## Frame inspection

Frame inspection accepts an Asset ID, one exact SourceStream ID, and one to eight
strictly increasing unique rational source times. It never chooses a hidden
"primary" stream for the caller. Each request time selects the last presentation
frame whose PTS is not greater than that time; when the stream has no earlier
frame, it selects the first frame. The result carries both requested time and
the selected exact source PTS, so seek rounding is never implicit.

The payload-pinned decoder exposes a bounded RGB24 capability, not a PNG file
writer. The adapter inventories integer frame PTS, compares `PTS * timeBase`
against requested times with exact rational arithmetic, and then decodes the
selected integer PTS. Input seeking may narrow the scan but is only an
optimization: an incomplete seek window widens until the selected floor frame
or first-frame fallback is proven. The API applies the closed orientation,
square-pixel scaling, and sRGB policy, then uses its in-process PNG encoder to
create canonical artifact bytes. A decoder seek approximation can therefore
never become the source-time truth.

Equivalent work deduplicates by Asset, accepted fingerprint, SourceStream,
ordered request times, and the closed `frame-srgb-png-v1` profile. Its immutable
frame-set artifact contains a canonical manifest that transitively binds every
PNG digest, byte length, normalized dimensions, requested time, selected source
PTS, orientation transform, and producer identity. Reprobing metadata does not
change this key while Asset/fingerprint/SourceStream identity is unchanged.

Agent media commands make their operational effects explicit:

- `asset inspect` is a bounded snapshot read. It reports current availability,
  facts, selected artifacts, and existing jobs but never schedules analysis as a
  hidden side effect;
- `asset frames` is an operational read requiring an active run and turn. It may
  reuse or schedule normalized interactive decode work and returns either leases
  or a durable job reference. It has no caller request identity because an
  expired lease is intentionally replaced by a new resource identity;
- `asset analyze` is a durable operational mutation with its own request
  identity and scope. Input selects only a closed registry analysis kind/profile,
  never an executable, model origin, source path, pool, priority, retry policy,
  or output destination.

None of these commands advances a creative revision. Equivalent decode or
analysis work deduplicates under the scheduler job-key contract.

For an Agent turn, an API-injected scratch adapter materializes at most eight
selected frames as normalized sRGB PNG, each with maximum long edge 1280 and
with a 32 MiB aggregate response cap, beneath the API-owned root for that exact
active Run/Turn. Neither CLI input, argv, environment, settings, Agent working
directory, nor HTTP body can select the destination. The CLI is still the sole
Agent-visible entry and returns a lease containing:

```text
resourceId
mimeType
byteSize
sha256
sourceTime
readOnlyPath
expiresAt
```

The path is a randomly named, bounded copy created only for a generic local Agent
image reader. It is not an original source, canonical artifact, data-directory,
or directory-enumeration contract. Expired leases are unreadable and a later
request creates a new resource identity. The first lease lifetime is five
minutes or the owning Turn's transition out of active state, whichever comes
first; cleanup is convergent across restart. Scratch lease rows model only live
delivery authority: expiry or revocation physically removes them after their
paths become unreadable. Durable job, artifact, and activity records carry the
history; scratch delivery bookkeeping never becomes an append-only audit log.

The command registry classifies `asset frames` as `operational-read`: it may
schedule or reuse durable decode work but creates no creative revision, caller
request identity, or Approval. A ready artifact returns leases; otherwise the
same command returns `accepted` with its durable MediaJob reference and bounded
activity cursor. Polling repeats the same normalized request rather than using
a second wait API.

The scheduled logical job gains a Run owner for attribution and ownership
release, but scheduling does not implicitly transition the Run to `waiting` or
declare a completion blocker. A terminal Turn immediately revokes its scratch
leases; canonical frame artifacts remain reusable product cache unless normal
artifact retention evicts them.

It never returns an unbounded video stream to the agent, grants arbitrary file
reads, or asks the agent to execute a decoder. Sampling policy and maximum result
size are enforced by Open Cut, independent of model requests.

## MediaJob

Each derived operation is a durable MediaJob:

```text
blocked -> queued
queued -> running -> succeeded
                  -> failed
                  -> cancelled
```

A job records project, asset, artifact kind, normalized parameters, producer
version, progress, attempt, terminal error, and output artifact ID.

Equivalent compatible jobs deduplicate by normalized work identity. A process
crash may retry a running job; publishing successful output is idempotent and
atomic. Partial files never become active artifacts.

A worker may combine fingerprinting with sequential decode for efficiency, but
publication still verifies the final strong fingerprint. File observation
changes during a read reject the attempt rather than producing an artifact for
ambiguous bytes.

The initial identify implementation is a hidden mode of the same signed API
executable. It is launched by the API scheduler as an isolated direct process,
not a sidecar and not an Agent command. This provides a payload-pinned hashing
executor before the separately distributed probe/decode toolchain is present.

Cancelling one AgentRun does not cancel a shared MediaJob unless no remaining
product owner references the job and cancellation is explicitly requested.

## Cold start and readiness

Media analysis is not part of API or aggregate runtime READY. After database
migration, the API becomes ready, recovers durable jobs, and publishes progress
through product activity.

An agent may reason from the artifacts currently available, start missing work,
or perform a bounded CLI wait. It cannot assume transcript completion before
inspecting job state.

## Collection and deletion

Creative tombstones do not delete original or derived bytes. Explicit collection
computes reachability from live projects, transaction history retained for undo,
active jobs, exports, and selected artifacts.

Referenced originals are never physically deleted by collection. Managed source
deletion, export overwrite, and irreversible artifact purge require creator
approval.

## Security boundary

- Decoders operate on creator-selected local media only.
- Media subprocesses receive bounded inputs and product-owned output directories.
- Canonical artifact paths never escape the API data directory; leased frame
  copies may enter only their owning run's isolated scratch directory.
- Agent commands accept product IDs and rational times, not arbitrary host paths.
- Agent-visible paths identify only bounded leased resources, never sources,
  canonical artifacts, exports, databases, or product data roots.
- Metadata and transcript text are untrusted content and never become executable
  commands or shell fragments.
- A malformed asset failure remains isolated from other project jobs.
- Missing, changed, or inaccessible source bytes update Asset availability;
  unsupported codecs, malformed containers, or invalid decoder output fail only
  their MediaJob and do not turn a successfully fingerprinted Asset offline.

## First vertical slice

The first slice requires metadata, bounded frames, waveform, transcript, and one
preview-compatible proxy path. It also requires signed on-demand model
acquisition and offline reuse. Shot detection and semantic indexing may be added
only after the footage-to-paper-edit acceptance scenario is deterministic.
