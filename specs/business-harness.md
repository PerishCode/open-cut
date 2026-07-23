# Business acceptance harness

Status: Business design baseline.

## Purpose

The business harness proves that Open Cut behaves as one durable creative
product across domain, media, CLI, UI, restart, and installed-delivery
boundaries. It does not create a privileged product path for tests.

The final black-box actor has exactly two Open Cut-facing inputs:

```text
the shipped Agent prompt
the installed open-cut executable on PATH
```

Every discovery, read, mutation, wait, and recovery step then crosses the public
CLI contract in `specs/agent-native.md`. The actor receives no endpoint, token,
schema file, database path, data directory, sidecar descriptor, or source-tree
import.

The harness launches the actor with the same allowlisted AppState environment as
the API Agent bridge. Project, sequence, and run IDs are scoped CLI defaults like
the executable search path; they are not a third callable interface, a business
grant, or a hidden schema.

## Harness layers

The acceptance strategy has distinct layers. Passing a lower layer never
substitutes for the installed black-box scenario.

### Domain invariants

Pure domain tests execute typed operations against in-memory state and verify:

- exact rational-time arithmetic and overflow rejection;
- canonical decimal-string wire round-trips beyond JavaScript safe integers;
- RFC 8785/domain-separated digest vectors and server-only UUIDv7 allocation;
- revision preconditions, conflict reporting, and idempotent retry;
- atomic cross-model Narrative, Sequence, and Alignment commits;
- inverse-operation undo as a new forward transaction;
- tombstone, alignment-staleness, and immutable-transcript rules;
- AgentTurn generation leases and stale-turn rejection;
- immutable proposal digest and approval-time atomic execution;
- proposal-scoped local reference allocation and idempotent durable-ID mapping;
- timeline track compatibility, range, and overlap validation;
- linked A/V operation scope and explicit source-stream selection;
- item-anchored Alignment movement, split remapping, and stale fallback;
- export pinning independent of later creative commits.
- ephemeral UI gestures and text checkpoints producing exactly one transaction;
- playback clock/revision pinning and deterministic caption derivation;

Property-based cases generate operation sequences and assert that no invalid
intermediate aggregate can commit.

### Persistence and recovery

API repository tests run the same ordered SQLite migrations and repositories as
the sidecar entry. They inject process loss at durable boundaries including:

- before and after an EditTransaction database commit;
- after commit but before a CLI result is received;
- during derived-artifact temporary output and before atomic publication;
- while a MediaJob or ExportJob is running;
- while an Approval or AgentRun is waiting;
- after an export pins revisions but before rendering completes.
- while a leased attempt generation expires and a replacement starts;
- during atomic Project genesis and approved purge boundaries.

Recovery must converge from durable state. Retrying a committed request identity
returns the current converged outcome without duplicate durable work;
unpublished bytes never become selected artifacts; and restart never implies
undo, approval, cancellation, or a duplicate job.

Persistence tests separately compare immutable journal heads, normalized
projections, revision cascades, request outcome mappings, and transactional
activity cursors after every injected crash boundary.

### Product ports and UI projections

Contracts tests drive the stable product read/write ports and reconcile activity
events against snapshots. UI tests verify that the same project state is
projected into assets, transcript, narrative, viewer, timeline, approvals, and
job progress.

They race snapshot-plus-cursor reads against SSE reconnect, duplicate and gap
activity delivery, and require bounded refetch. Viewer tests exercise session-
bound opaque MediaLeases, `HEAD`/Range seeking, expiry renewal, and process-
instance invalidation through the real same-origin proxy chain. Sequence UI
acceptance proves that the persistent transport and Timeline command one shared
playhead: Timeline seek moves the media actuator, playback observations settle
to the declared frame grid without accumulated float drift, frame-step is exact,
and a creative Timeline gesture pauses before planning.

These tests may use in-process adapters, but Web still imports product
communication only from `packages/contracts`. UI test convenience is not an
Agent-facing interface.

Creator source-input tests exercise the shared file field through selection and
drop, then prove the browser `File` is sealed by preload/main into a one-shot
owner-bound token before Contracts calls the same-origin platform port. Scans
cover renderer state, request bodies, responses, and diagnostics: the fixture
name may be display metadata, but its absolute path must appear only in the
main-owned internal SourceGrant registration call. Native picker cancellation,
token expiry, owner mismatch, untrusted origin, and the Mac App Store
picker-only branch are typed cases.

### CLI conformance

The versioned product CLI is tested as an external process. Conformance begins
at root `--help`, recursively discovers child help, and verifies that:

- every help call returns one versioned JSON discovery document without starting
  the product;
- every advertised leaf accepts only its documented flags and stdin shape;
- every normalized non-help command returns exactly one valid versioned JSON
  envelope, including business failure states;
- diagnostics never contaminate stdout;
- typed status, identity, cursor, revision, and error fields are stable;
- mutations require revision preconditions and a request identity;
- the same identity and input are idempotent, while identity reuse with different
  input is rejected;
- command-body, request-input, and immutable-effect digests are independently
  domain-separated and detect mutation at their respective boundaries;
- waits are bounded and resume from returned activity cursors;
- no help or result exposes internal transports, credentials, or data paths.

AppState cases prove the declared default, setting/launch parameter, environment,
and argv precedence; immutable isolation across concurrent Agent launches;
resolved context echo; project/run relationship validation; and that context
selection never creates authorization.

Authorization cases pair an installation public key through Creator UI, keep the
private key outside Agent context, verify signed challenge consumption, reject
replay/revocation, and prove that raw loopback HTTP cannot bypass the CLI or
first-party UI session.

Scope-upgrade cases prove that a pending new scope preserves established read
authority, approval increments the same grant revision and principal, and an old
challenge cannot cross that revision boundary. Run cases prove authorizing actor
binding, expected-generation resume takeover, stale-turn rejection, explicit
complete with blocker checks, and cancellation without transaction rollback or
shared-job cancellation.

First-party cases use a role-separated UI key, prove Electron-main-only session
custody and proxy injection, reject old cell/API/Web/Electron instances, and
scan renderer-visible storage, responses, logs, and diagnostics for credential
leakage.

The conformance runner may know generic CLI grammar and envelope assertions. It
must not compile in a private copy of the command tree or operation schema; the
active payload help is the discovery source.

### Installed product acceptance

The delivery harness first installs and starts the platform product through
`oc-control harness install|run`. A separate business actor then receives only
the prompt, stable CLI on `PATH`, and safe AppState defaults. `oc-control` does
not gain project, asset, timeline, or Agent semantics.

The business actor must not read the delivery receipt. Test orchestration may
use the receipt to locate the stable installed executable before handing that
path to the bridge environment.

Installed fixture setup drives the public Creator surface. Desktop automation
may attach a real path-backed `File` to the shared file field or operate the
native picker, but it may not call a SourceGrant API, preload method, IPC
channel, database, or test-only route directly. This setup authority ends after
the Creator gesture; the business actor still receives only the shipped prompt,
stable CLI on `PATH`, and safe AppState defaults.

Interactive packaged delivery checks may explicitly request a loopback-only
Chromium DevTools port through a harness environment value inherited by the
installed host. Electron accepts it only for `packaged|harness` lifecycle mode
with interactive presentation. The port is UI automation transport, never a
product/Agent surface, and is removed from the actor environment together with
receipt, sidecar, and authority material.

Native export delivery acceptance remains outside that renderer transport. The
controller uses CDP only to click the shipped Creator `Save As…` action, then a
target-local external OS accessibility adapter operates the real native Save
panel. The destination path belongs only to the controller; it is absent from
the actor environment, product evidence, Web/API/Agent state, and product logs.
After the shipped UI reports its safe receipt, the controller independently
requires a regular file whose byte size and SHA-256 equal the verified
ExportArtifact already observed through stable CLI. It then clicks shipped
`Reveal in folder` and requires only the safe display-name result. No test
route, IPC channel, preload method, dialog bypass, injected DestinationGrant,
or internal API is permitted.

This native dialog slice runs only after a successful production installed
export. Each public target must implement and qualify its own external OS
adapter on that target's runner; absence is a hard lane failure, and another
target's accessibility result cannot substitute for it. The first adapter is
macOS; Windows and Linux remain explicitly unsupported until their native
delivery runners exist.

Before the complete footage-first scenario, one installed bootstrap slice must
prove the boundary itself from an empty receipt-owned data root:

1. Creator UI creates a Project and attaches a real path-backed fixture through
   the public shared file field;
2. the separate actor recursively discovers root, group, and leaf help through
   the stable CLI found on `PATH`;
3. its first business read returns `pairing-required`, Creator approves that
   exact pending installation through the public UI, and the actor retries;
4. CLI `project list`, `project show`, and `asset list` observe the same Project,
   main Sequence, and online Asset created through the Creator surface;
5. after recursively discovering `run begin/show`, the actor creates one bound
   standalone AgentRun and confirms the same active Run and generation-one Turn
   through a separate durable read;
6. through discovered `asset inspect`, the actor waits for identify, probe, and
   proxy work to succeed, cross-checks the exact media-facts and proxy Artifact
   identities, and confirms waveform/transcript remain typed blocked on the
   unavailable executor/model rather than treating them as product failure;
7. the actor discovers bounded narrative/entity reads and the edit command group,
   then reads the exact Project and root Narrative-node revisions through CLI;
8. it sends one `insert-authored-text` proposal as strict JSON exclusively to
   `edit propose --input -`, with explicit `spoken` purpose, canonical language,
   complete plain text, and an exact root-node revision precondition;
9. it applies the exact immutable proposal digest under a separate request
   identity; and
10. narrative readback, entity readback, proposal status, and transaction history
   independently confirm the same committed AuthoredText and transaction.

The pass evidence contains only durable business identities and typed state,
including the Project revision, Run/Turn, Proposal, EditTransaction, and
AuthoredText identities, the bounded audio SourceStream, media-facts/proxy
Artifact identities, and typed media-job states. It contains no fixture path,
receipt, endpoint, token, SourceGrant material, or transport details. This
installed seed slice is a prerequisite, not a substitute, for the complete
creative scenario below.

Installed acceptance is split into two delivery lanes:

- every push and pull request runs the fast empty-data-root seed slice and must
  observe the transcript job blocked by the typed `model-required`
  prerequisite;
- scheduled, published-release, and explicitly requested manual runs additionally
  execute the production-model slice. Creator UI requests `Download for offline
  use`, waits for the public resource projection to become `ready offline`, and
  only then may the separate actor poll `asset inspect` and invoke bounded
  `transcript read`.

The production lane starts with a fresh receipt-owned data root and does not
restore a prior product datadir or acquired model across runs. Such a cache
would bypass the acquisition, verification, publication, and prerequisite-
resume behavior being proved. A future optimization may cache only through a
product-owned verified transport-cache contract; CI may not substitute a model
origin, inject published resource bytes, or authenticate the catalog itself.

Production transcript evidence requires a succeeded transcript job, a matching
ready Artifact, its exact source stream, non-empty bounded segments and tokens,
detected language, recognition profile, and model version. A failed job must
surface its typed terminal error code through `asset inspect`; neither the actor
nor the harness may inspect repository tables or stderr to recover it.

The same production lane must continue from that public evidence without an
actor reset. It derives one token-bounded SourceExcerpt range whose token lexical
bytes exactly reconstruct the cited Segment, commits it through the sole
`edit propose|apply` write path, and reads back the immutable fingerprint,
Artifact, SourceStream, Segment, correction-revision, effective-text, and
`exact` evidence closure. It then submits one body-carrying read-only
`edit derive-rough-cut` query for explicit video/audio Tracks and SourceStreams,
carries the returned operation and exact preconditions unchanged into a second
proposal, and confirms the committed video Clip, audio Clip, LinkGroup,
two-target exact Alignment, and Sequence window through bounded CLI reads. It
then recursively discovers `sequence frames` and `export start|show|retry|cancel`,
proves two exact leased Sequence frames, starts the pinned
`webm-vp9-opus-v1` export, and observes a verified immutable artifact through
CLI only. Export evidence contains no destination, artifact path, RenderPlan,
renderer, material, datadir, or runtime provenance.

After that verified export, the production lane saves the exact artifact
through the Creator native dialog, independently verifies the destination
bytes, and exercises process-local Reveal. Safe evidence may record only the
display name and `saved-revealed` status, never the destination path or
DeliveryReceipt.

Planning revisions live in the returned proposal precondition set. The
normalized `roughCutItems` operation repeats only its SourceExcerpt, Track, and
SourceStream identities; the actor must prove both structures and must not
require or tolerate a second divergent revision field inside the operation.

## Canonical footage-first scenario

The first release-blocking scenario is:

1. start from an empty isolated product data root and installed stable CLI;
2. use the public Creator project form and file field/native picker to create a
   project, register a small creator-authorized SourceGrant/Asset fixture, pair
   the installation CLI, and authorize acquisition of the signed local
   transcription model;
3. create the AgentRun and first AgentTurn, then launch the business actor with
   only the shipped prompt, stable CLI on `PATH`, and safe AppState context
   environment;
4. have the actor discover the project and observe independent metadata, frame,
   waveform, proxy, and transcript jobs;
5. inspect bounded leased frames with a generic image reader and read transcript
   segments by product identity;
6. create a restricted paper edit containing source excerpts and authored
   intent;
7. propose and atomically apply one transaction that assembles the `main`
   sequence and exact alignments;
8. preview the committed sequence projection;
9. retry the apply request and prove no duplicate creative state;
10. commit undo and prove history advances while the visible edit reverses;
11. reapply a valid edit, start a pinned export, restart the product during the
    job, and verify the final artifact and recorded pin;
12. remove or change a referenced source and verify typed degraded state without
    loss of project history.

No step may inspect SQLite, derived artifact directories, internal HTTP, or
sidecar state to decide business success.

## Media fixtures

Small exact fixtures are either generated deterministically from textual recipes
or embedded as pinned bytes when generation would add an ambient speech
synthesizer. A fixture manifest records expected stream facts, exact duration,
frame markers, spoken transcript, and checksums. Large binary media is not
committed to the repository.

The fast installed slice generates a stereo WAV. The production-model slice
embeds a 14,125-byte WebM with one 160x90 VP9 video stream and the exact mono
Opus speech stream used by the earlier audio-only fixture. Its SHA-256 is
`4c9427131a168f7a31203530fd90734e221d928963c229faceb5dcc117d1316a`;
decoding either fixture to 16-kHz mono S16 produces SHA-256
`a0344f540bb85ae28182f3af2587e9847d90b1dcfc2d2711bcca7f6b9cf71aa7`.
The four-second video bound deliberately covers the complete recognition range,
and the speech retains stable lexical anchors for “Alpha bravo”, “spoken
ideas”, and “editable story”. The recognized punctuation and capitalization are
not treated as a quality oracle; normalized lexical anchors, exact product
identities, typed state, and immutable transcript structure are.

The canonical fixture contains visible time markers, two distinguishable scenes,
stereo audio, and a short unambiguous spoken phrase. A second malformed fixture
proves decoder failure isolation. Platform outputs are compared by declared
semantic tolerances, while timestamps, selected ranges, revisions, and
transaction results remain exact.

Additional generated cases cover changed fast file observations with equal
SHA-256, content mismatch, alternate A/V streams, and HDR-to-SDR reference
conversion. Baseline export verification asserts the complete
`webm-vp9-opus-v1` container, codec, pixel, color, frame-rate, canvas, and audio
contract. A release advertising `social-mp4-v1` must additionally run its full
MP4/H.264/AAC conformance and compliance gate.

Sequence renderer release qualification separately covers VFR floor/first-frame
selection, repeated and reordered Clip ranges, multiple visual/audio tracks,
transparent gaps, crop/reframe, fixed-point gain/mix, caption fallback and
missing glyphs, explicit `und` and regional CJK language-to-face selection,
cluster-preserving Arabic/Indic fallback, audio-only/video-only/caption-only
plans, and non-grid tail padding. Every same-build/target byte-stability fixture renders at least twice
and compares the complete artifact digest. Cross-target runs compare semantic
reference outputs and exact media facts rather than encoded byte identity.
Harnesses also require success/result/output digest agreement, preserve a typed
missing-glyph failure through the private helper boundary, and classify a
missing, mixed-status, unknown-code, or malformed result as process/output
failure rather than parsing stderr as product state.

## Agent qualification

Deterministic domain, persistence, CLI conformance, and scripted scenario drivers
are commit gates. A separate nightly live-Agent qualification begins with no
command knowledge beyond the shipped prompt, stable CLI on `PATH`, and scoped
AppState defaults, then attempts the canonical creative goal.

Live-Agent evaluation records help calls, command invocations, typed outcomes,
approvals, and committed transaction IDs. It scores protocol compliance and
creative completion separately. Model prose is never used as the oracle for
project correctness; durable Open Cut state and export results are.

Release candidates run a declared matrix of supported Agent adapter, Agent
version, prompt version, platform, and repeated attempts. Release qualification
uses a published protocol-compliance and creative-completion threshold rather
than one stochastic pass.

The first-release matrix contains only `codex-cli-v1` plus the deterministic
standalone CLI driver. It records the observed Codex version and proves stdin
prompt, JSONL framing, user config/rule isolation, scratch-only inline
permission profile, stable-resolver-only `PATH`, loopback-only limited network
with direct API authorization rejection, native-session reset/resume, and
process-tree cancellation on every supported target. Arbitrary executable
templates are not a qualification surface.

Bridge cases additionally prove one composer request creates one generation,
request replay converges, a second in-flight submission is rejected, pairing
approval does not bind an actor until the next signed CLI call, and a grant or
Run mismatch cannot bind. Normal response, detach, interrupt, terminal cancel,
resource limit, native resume, and explicit conversation reset each preserve
their distinct Run/Turn state. Conversation reload contains only Creator and
completed Agent messages; raw JSONL reasoning, commands, stderr, paths, and
partial deltas remain absent from SQLite, OpenAPI, Contracts, activity, and CLI.

## Failure and conflict matrix

Release coverage includes at least:

- stale project context with unchanged touched entities succeeds;
- a stale touched entity returns a conflict without partial mutation;
- duplicate delivery after lost CLI output returns the converged logical request
  result without duplicate work;
- cancellation leaves prior committed transactions intact;
- a waiting approval survives restart and cannot be self-approved by the agent;
- missing, changed, and unreadable sources remain distinct;
- SourceGrant restart, revocation, relink, and Electron-peer loss never expose a
  path or require the peer for media access;
- an unavailable transcript does not block manual or frame-based editing;
- a missing model durably blocks only compatible transcript jobs and resumes
  them after signed resource acquisition;
- one invocation never mixes payload versions, while a cross-release AgentRun
  returns a typed incompatibility instead of old-write emulation;
- Agent bridge loss preserves receipts and rejects late writes after cancellation;
- a superseded AgentTurn cannot write even if its process remains alive;
- approval commits only the exact immutable proposal digest and converges the
  original request identity;
- resource acquisition rejects any origin or digest absent from the active
  authenticated catalog;
- export failure identifies the pinned missing source and never rewrites the
  sequence.
- an expired worker attempt cannot publish after a replacement generation wins;
- API READY and lifecycle shutdown remain independent of queue drain;
- Project archive/tombstone preserves history and referenced originals, while
  purge requires the exact current impact approval;
- Viewer media leases fail when copied across session or process boundaries and
  never become an Agent capability.

## Architecture assertions

Repository guards and black-box tests reject:

- an Agent-facing MCP, SDK, HTTP, socket, database, or second Open Cut Agent
  entry executable;
- business knowledge in launcher, runtime runner, broker, or `oc-control`;
- direct CLI imports of app-private source;
- duplicated business command schemas outside `product/command` and transport
  adapters;
- Web transport outside `packages/contracts`;
- unsafe JSON-number encodings for exact time, revision, or activity cursor;
- creative writes attributed to a background `system` actor;
- silent mutation auto-chunking or behavior-affecting defaults outside a digest;
- a product worker sidecar or Agent-controlled scheduler priority/executable;
- test-only creative mutations or direct database fixture insertion in the
  black-box scenario;
- shelling out to an unpinned system decoder or encoder;
- original-media, canonical artifact, export, database, or data-root paths in
  Agent-visible results; bounded leased frame paths are the sole exception.

## Evidence

Each run emits a machine-readable evidence bundle containing product and CLI
versions, platform target, fixture manifest identity, ordered CLI invocations,
redacted envelopes, restart points, transaction and job IDs, export pin, and
assertion results. It contains no secrets or absolute creator paths.

Evidence is diagnostic output, not a new product truth source. A failed run can
be reproduced from declared fixture and invocation identities without copying a
project database.
