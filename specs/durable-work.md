# Durable work and approval semantics

Status: Business design baseline.

## Boundary

Open Cut persists business execution facts that must outlive one Agent or CLI
process. It does not persist or impersonate the local Agent's private model
context.

Seven state-machine families live beside the Project aggregate:

- `AgentRun` groups one creator intent, Agent command receipts, waits, and
  committed edit transactions;
- `AgentTurn` represents one generation-scoped local Agent process attachment;
- `MediaJob` produces an immutable analysis artifact;
- `SequencePreviewJob` prepares exact inputs, pins one RenderPlan, and produces
  one immutable Viewer preview artifact;
- `ResourceJob` acquires one signed installation-scoped ProductResource such as
  a local transcription model;
- `ExportJob` renders one pinned sequence revision;
- `Approval` authorizes one normalized high-impact request.

Project work references projects and entities; ProductResource work is
installation-scoped. Both advance on their declared `activitySequence`, not
`projectRevision`. Only an EditTransaction mutates creative state.

## AgentRun

The API bridge normally creates or binds a run after selecting a project and
creator intent; a standalone CLI flow may begin one explicitly. Every later
Agent mutation resolves that `runId` through AppState or argv; reads may attach
it so Open Cut can record the evidence used for a proposal.

Bridge launch may precede first-time CLI pairing. Such a run starts with a
Creator initiator but no asserted Agent actor and cannot perform creative writes:

```text
authorizing -> active | failed | cancelled
active -> waiting -> active
active -> paused -> active
active -> completed
active -> failed
active -> cancelled
waiting -> paused | completed | failed | cancelled
paused -> completed | failed | cancelled
```

The first proof-of-possession request associates its pending grant with the
authorizing run and turn. After Creator pairing approval, the first successfully
authorized CLI request atomically binds that exact durable Agent principal and
moves the run to `active`. A standalone authorized `run begin` creates a bound
active run directly. Run/turn IDs in environment remain selection only; no launch
credential or hidden capability is introduced.

`waiting` records a typed reason and referenced job or approval IDs. It does not
mean that Open Cut owns a suspended model process. A later invocation can read
the durable run summary, observe the resolved blocker, and continue through the
same CLI.

If an active Agent process exits without explicitly completing the run, the run
becomes `paused` with reason `agent-detached`. A run already waiting on a durable
job or approval remains `waiting`. Neither state claims model failure or creative
completion.

An AgentRun records:

```text
id
projectId
intent
initiatedByCreator?
actorIdentity? (required after authorizing)
status
waitingReason?
startedProjectRevision
latestObservedProjectRevision
commandReceipts[]
transactionIds[]
jobIds[]
approvalIds[]
activityCursor
createdAt / updatedAt / completedAt?
```

A command receipt stores normalized command identity, request identity, selected
input entity and artifact IDs, typed outcome, and resulting durable IDs. It does
not copy arbitrary model messages, chain-of-thought, media bytes, secrets, or
absolute source paths.

One receipt belongs to exactly one Run and Turn. `none` commands do not allocate
an ordinal. `evidence` and `outcome` receipts use an independent monotonically
increasing Turn ordinal and are read through a bounded `TurnReceiptPage`.
Idempotent business request replay reuses the original outcome receipt; an
attached evidence read without a request identity records a new observation.

Receipts are immutable invocation-time evidence, not mutable Job or Approval
projections. `accepted`, `waiting`, and `approval-required` retain their original
meaning after the referenced state machine advances. Creator surfaces resolve
the referenced durable object separately when current state is needed.

An authorized current-Turn command that has crossed application validation and
returns a valid product envelope records its exact business status, including a
no-effect conflict, not-found, unavailable, incompatible, invalid, or failed
result. Invocation parsing, canonicalization, authentication, pairing, scope,
expired challenge, and stale writer authority are not product executions and do
not enter this ledger. A no-effect receipt must commit before its result is
returned, but carries revision or cursor evidence only when that exact business
result supplied it; the receipt layer never performs a second projection read
and presents it as the original observation.

Completing or cancelling a run is explicit. `complete` is rejected while the run
has an unresolved declared blocker and never derives success from presentation
or edit count. `cancel` releases this run's job ownership references but does not
undo transactions or transitively cancel shared work. Loss of the Agent process
leaves the run durable and recoverable; it neither marks success nor rolls back
edits.

## AgentTurn

Each bridge or standalone Agent invocation creates an AgentTurn:

```text
id
runId / projectId
generation
adapter / agentVersion / promptVersion
nativeSessionId?
status: starting | active | detached | completed | failed | cancelled | superseded
startedAt / endedAt?
```

One AgentRun has at most one active writer turn. Bridge launch injects its
`turnId`; standalone `run begin` creates the first turn. `run resume` names the
expected current generation and atomically marks its `starting`, `active`, or
`detached` turn `superseded` while creating the next generation. This explicit
takeover recovers an orphaned standalone process without treating an inactivity
timeout as proof of process death. Concurrent takeover attempts cannot both
succeed.

Every Agent mutation validates project, run, active turn ID, and turn generation.
Resuming supersedes the prior write lease, so a late command from an old process
returns `stale-turn` without mutation. Reads may remain available for diagnosis.
Multiple independent AgentRuns may edit one Project and resolve ordinary
revision conflicts normally.

For a bridge-owned Run, a Creator composer submission is the sole creator of a
new generation. Its exact message and request identity are committed with the
turn before the native process starts. A normal native response completes the
turn and pauses the Run at `awaiting-creator`; process loss detaches it. The
Creator may interrupt a turn without cancelling the Run, while terminal Run
cancellation remains a separate explicit transition. Agent-facing `run resume`
cannot take over a bridge-owned Run. In bridge launch context, `run begin`, `run
resume`, and `run cancel` are rejected; `run show`, bounded `run wait`, and `run
complete` operate on the same durable Run through safe product projections.
`run wait` observes strictly after an explicit activity cursor, uses the signed
effective AppState wait policy as its only bound, and returns the current Run on
activity, terminal state, or timeout. It never launches, resumes, completes, or
cancels a native or durable Turn.

The adapter may retain a Run-scoped private native home derived from API datadir
while an active Project has a non-terminal Run. That home is disposable cache,
not Run or Project state, and its loss never invalidates a receipt. Terminal Run
transition and Project archive/tombstone/purge collect it while keeping the safe
ConversationLedger until Project purge.

`nativeSessionId` and resolved adapter paths are repository-private bridge
metadata. Agent-facing Run reads expose product generation/status only;
Creator-only adapter diagnostics may expose a safe adapter ID, qualified version,
prompt version, and closed availability state.

## MediaJob

MediaJob states are:

```text
blocked -> queued
queued -> running -> succeeded
queued -> cancelled
running -> failed | cancelled
```

Crash recovery may create another attempt under the same logical job. A terminal
failed attempt never selects partial output. A creator retry creates a new job
related to the failed one unless the original normalized request was already
committed and only its result delivery was lost.

Equivalent compatible work may be shared by multiple runs. Ownership references
are explicit, so cancelling one run cannot accidentally cancel shared work.

MediaJob, SequencePreviewJob, ResourceJob, and ExportJob are typed views over
the generic WorkJob core. Their attempts are claimed, isolated, recovered, and
atomically published by the API-internal SQLite lease scheduler in
`specs/job-scheduler.md`. No worker sidecar or Agent-controlled priority exists.

`blocked` carries a bounded typed prerequisite set rather than one lossy reason.
Initial prerequisite kinds are `fingerprint-required`, `facts-required`,
`model-required`, and `executor-required`; each may reference its producing job,
resource, or closed capability. A blocked job consumes no worker attempt.
Reconciliation removes satisfied prerequisites and moves a job to `queued` only
when the set is empty. Therefore `queued` always means currently claimable by a
registered executor, not merely durable intent awaiting an unknown capability.

## Render-material attempt leases

Preview and export attempts read immutable media artifacts after their binding
transaction has committed. Before a canonical artifact path crosses into the
contained renderer, the API atomically validates the exact running attempt and
pins every plan input in `render_material_leases`. The parent WorkJob attempt
lease is the only liveness clock: heartbeat renewal keeps the pins live;
success, failure, cancellation, abandonment, or expiry releases them.

These are internal attempt leases, not Agent Turn scratch-delivery leases.
Artifact eviction, quarantine, and repair must fail closed while any live
attempt pins the artifact. A root ExportJob never transfers this lease or
cancellation authority to its shared render-input producer jobs.

## SequencePreviewJob

A SequencePreviewJob is Creator-only operational work over one exact Sequence
revision. It fixes its SourceStream/resource producer jobs before claim, remains
blocked until every selected immutable input is ready, then atomically binds one
RenderPlan digest before rendering. The first closed response projection is
`empty | preparing | ready | failed`; `empty` creates no job.

The job stores a canonical render-relevant snapshot at creation. Dependency
completion may happen after the moving Project head has advanced, but first-plan
compilation uses only that stored snapshot and the exact immutable producer
artifacts. Once a RenderPlan is bound, every retry reuses it.

Later creative revisions do not mutate or cancel the job. Viewer ownership may
be released under ordinary shared-work cancellation policy, but revision
adoption is an explicit playback decision. Successful output is a typed,
project-owned, cache-evictable SequencePreviewArtifact rather than a MediaArtifact
or ExportArtifact. It is never exposed to the Agent command tree.

Continuation is read-only observation of the current retry-chain tail. An
explicit Creator retry accepts only a failed or cancelled tail and creates a new
related WorkJob from the stored intent. It does not reuse the request shape for
moving-head preparation. Failure projection carries a closed recovery action;
deterministic invalid plans, nondeterministic output, and incompatible runtime
closures never advertise a blind retry.

## ProductResource and ResourceJob

A ProductResource is installation-scoped, outside every Project aggregate. It
records kind, logical name, version, signed manifest identity, content digest,
compatibility, local state, and product-owned byte reference.

The active payload's authenticated product-resource catalog is the only accepted
manifest source. Its authenticity is inherited from the signed release; the API
does not create an independent trust root. ResourceJob normalizes and persists
the exact catalog entry and entry digest used for acquisition. Origin, digest,
destination, pool, and retry policy are never request parameters.

A ResourceJob verifies and atomically publishes one such resource:

```text
queued -> running -> succeeded
queued -> cancelled
running -> failed | cancelled
```

Equivalent acquisition deduplicates across projects. Projects and MediaJobs hold
explicit wait references but do not own the shared bytes. Creator UI initiates
or authorizes acquisition; the Agent may inspect and wait on the returned job ID
but cannot choose an origin, destination, or untrusted manifest.

The first public projection is the Creator-only
`open-cut/product-resource-snapshot/v1`. Its closed states are `not-acquired`,
`queued`, `acquiring`, `ready`, `failed`, and `cancelled`; it exposes logical
identity, version/profile, expected size, progress, job/resource identity, and a
bounded failure code. It never exposes origin, content digest, filesystem path,
or datadir. Acquisition uses a Creator request identity and converges on the
current exact ready or live job. These routes are deliberately absent from the
Agent command registry and product CLI.

The downloader accepts HTTPS only, disables content encoding, and follows at
most three credential-free HTTPS redirects without fragments. Every hop is
revalidated; redirecting to HTTP, embedded credentials, a fragment, or another
redirect beyond the bound fails closed. It requires exact length plus SHA-256.
Resume is allowed only with a strong ETag,
`If-Range`, and exact `Content-Range` bound to the same catalog entry. Verified
bytes publish atomically to the API datadir under a service-generated resource
ID; poisoned or losing attempt bytes are deleted. Publication and the ready row,
job success, activity, and compatible `model-required` reconciliation commit as
one application outcome.

API cold start keeps resource reconciliation bounded: it verifies the exact
symlink-free directory shape, regular file, and expected size, removes orphan or
invalid partial publication trees, and never hashes a multi-gigabyte model on
the startup path. Full digest verification occurs before publication and again
at the consuming transcription executor boundary; a mismatch invalidates the
resource and cannot become recognition input.

Consumer-bound corruption is one atomic work transition: the resource becomes
`invalid`, all active attempts bound to that resource become `abandoned`, each
nonterminal transcript job returns to `blocked` with `model-required`, and both
installation resource activity and Project media activity are appended. No old
artifact is erased and no job silently binds a different model.

Resource activity is installation-scoped and is projected into each waiting
Project's ledger through explicit references. Removing a reproducible resource
does not erase transcript artifacts already produced from it, but active jobs
and declared offline retention prevent collection.

The first release treats an acquired default transcription model as declared
offline retention. It is not silently evicted; removal is an explicit Creator
operation with impact. Later bounded-cache policy may add opt-in model eviction
without changing model identity or acquisition authority.

## ExportJob

The command grammar, lineage, full-quality input, preset, artifact, and
recovery contract are normative in `specs/export.md`.

ExportJob uses the same queued, running, succeeded, failed, and cancelled states
but additionally persists its project, sequence, revisions, normalized preset,
input Asset complete SHA-256 identities, selected artifact versions, canonical destination
identity, and renderer version before work starts.

Cancellation is best-effort for active rendering and never changes the pinned
sequence. Success publishes one immutable export artifact atomically. Retrying a
request identity cannot render or overwrite a second output.

The first-slice destination is a project-owned export area below API product
data. Agent results return an ExportArtifact identity and facts, never its
canonical path. Agent-started jobs are owned by AgentRun; Creator-started jobs
are owned by the durable Creator actor. UI session is authorization, never a
durable owner. Creator `Save As` or reveal actions are UI-owned; Electron main
holds the one-shot native DestinationGrant and consumes a session-bound API
artifact-read lease. Copying to an arbitrary destination and overwriting
existing bytes use explicit OS authorization and Creator intent.

Creator history projects one row per root ExportJob lineage with the current
tail and an exact attempt count. Retry jobs retain their own durable identities,
attempts, owners, and artifacts, but do not become parallel history cards.
Explicit Creator deletion only tombstones the exact ExportArtifact as
`deleted`; it never deletes or rewrites WorkJob/Attempt lineage and never
cascades to shared render-input producers.

## Approval

Approval states are:

```text
pending -> approved -> consumed
pending -> denied | expired | cancelled
approved -> expired | cancelled
```

An approval contains creator-readable impact, normalized request digest, actor
identity, project and entity preconditions, expiration policy, and the exact
capability to consume. Approval is not a reusable blanket privilege.

The Agent may create an approval requirement indirectly by proposing or applying
a protected action, then use only `approval show|wait`. It can never approve,
deny, widen, or manufacture an approval through the CLI.

Approval and mutation are race-safe. Consumption verifies the same request
digest and current entity preconditions in the commit boundary. A creative
change that invalidates the approved request returns a typed stale approval and
requires a new decision.

For a protected EditProposal, creator approval is the execution boundary. The
product atomically revalidates the immutable proposal, consumes the Approval,
and commits the EditTransaction or starts the exact approved Job. The Agent does
not need to wake and retry the action. `approval wait` and the Proposal or Job
projection expose the final outcome. Replaying the original request identity
returns its immutable `approval-required` result and the same Approval ID.

Protected first-slice actions include:

- deleting managed source bytes or irreproducible artifacts;
- overwriting an existing export destination;
- registering or relinking an arbitrary host source path through an Agent call;
- invoking a paid or external service;
- any operation explicitly classified irreversible by its active-payload help.

Reversible local EditTransactions do not require per-transaction approval after
the local CLI has been paired.

## Activity log and waiting

Every state transition appends one activity record with a strictly increasing
cursor inside its project or installation scope. The current row state and
activity append commit atomically. Consumers reconcile from a snapshot plus
cursor, then request records after that cursor.

Agent-facing waiting is always bounded polling through the CLI:

```text
show current durable state
wait after a cursor until change or timeout
repeat using the returned cursor
```

A timeout returns `waiting`, not failure. CLI termination has no cancellation
semantics. Internal SSE or subscriptions may optimize UI and CLI adapters but
are never exposed to the Agent.

## UI projection

The UI projects runs, jobs, exports, and approvals into one activity ledger. A
creator can inspect intent, current phase, requested impact, command outcomes,
committed diffs, and terminal errors without reading Agent prose or logs.

The primary canvas remains assets, narrative, viewer, and timeline. The ledger
explains and controls work; it is not a second creative-state editor.

## Invariants

- No state machine transition mutates creative state implicitly.
- No AgentRun claims ownership of the Agent's conversation or model context.
- No waiting CLI process is required for durable progress.
- No approval can authorize input other than its normalized digest.
- No run cancellation undoes a transaction or transitively cancels shared work.
- No terminal output becomes visible before atomic publication.
- No model-written prose determines whether a durable action succeeded.
- API readiness never waits for the durable queue to drain.
