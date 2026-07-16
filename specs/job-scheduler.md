# Durable job scheduler

Status: Business design baseline.

## Ownership

The product API owns one internal SQLite-backed scheduler for all durable
product work. It is an application service,
not a sidecar, daemon, launcher concern, or Agent tool. Adding a worker sidecar
to run business jobs is forbidden in the first release.

The persistence core is a generic `WorkJob`/`WorkJobAttempt` lease state machine.
Domain families remain explicit typed details rather than nullable columns or
one polymorphic result table:

```text
WorkJob core
  identity / project-or-installation scope / kind / state
  pool / priority / logical key / parameters / producer
  progress / cancellation / retry relation / timestamps / terminal diagnostic

MediaJobDetails
  asset / media parameters / selected MediaArtifact

SequencePreviewJobDetails
  sequence revision / resolver+compiler+renderer identities / RenderPlan
  selected SequencePreviewArtifact

ResourceJobDetails / ExportJobDetails
  their own closed inputs and typed results
```

Generic attempts, owners, and prerequisites reference `WorkJob`. Media artifacts,
RenderPlans, SequencePreviewArtifacts, ProductResources, and ExportArtifacts stay
typed. A fake Asset, a weak `subject_kind/result_kind` foreign key, and a second
family-specific scheduler are forbidden.

AgentRun and AgentTurn are durable state machines but not scheduler jobs. The
API Agent bridge owns their external process attachment under the separate
turn-generation contract; it may use the same process-isolation primitives.

API READY means migrations, repositories, endpoint, and scheduler recovery are
ready to accept work. It never waits for the queue to drain or for blocked
resources to become available.

The first scheduler worker is intentionally single-pool and claims only the
closed set advertised by the validated API-local executor registry. The initial
self executor registers `identify`; the first validated FFmpeg manifest adds
`probe`. This is a correctness slice, not the final fairness implementation.
Kinds without a registered active-payload executor remain durable and blocked
on `executor-required`; they are not failed and are never run with an ambient
system tool. Startup reconciliation queues them only after a validated
active-payload capability is registered.

## Logical job and attempts

A logical job stores normalized intent, deduplication key, owners/waiters,
priority class, a bounded typed prerequisite set, cancellation request, terminal outcome, and
selected immutable result. Execution uses append-only attempts:

```text
JobAttempt
  id / jobId / generation
  state: leased | running | publishing | succeeded | failed | abandoned
  leaseOwner
  leaseExpiresAt
  heartbeatAt
  startedAt / endedAt?
  normalized executor version
  temporary output identity
  typed diagnostics
```

Only one unexpired attempt generation may publish for a logical job. Lease
claim, generation advance, state transition, heartbeat, cancellation
observation, and terminal publication use short SQLite transactions.

A process crash expires the lease. Recovery marks the attempt abandoned and
either queues a new generation or returns a typed terminal failure according to
the job's retry policy. It never assumes a process that lost its lease still
owns output.

The first local deterministic policy is `local-deterministic-v1`: at most three
attempt generations may be created for one logical job after lease loss or API
process interruption. A third abandoned generation makes the job terminal with
`attempt-limit-exceeded`. A validated executor result or deterministic process
failure is terminal immediately; it is not disguised as a transient retry.
Creator preparation may create an explicit successor logical job with
`retry_of_job_id` after the blocking input, resource, or capability has changed.
Agent or UI input cannot select or enlarge this policy.

## Pools, priority, and fairness

The scheduler has bounded resource pools:

- `interactive-cpu` for short frame/probe requests;
- `io` for hashing, copies, and local artifact access;
- `cpu` for transcription, proxy, software render, and analysis;
- `gpu` for explicitly supported equivalent acceleration;
- `network` only for authenticated ProductResource acquisition.

Pool sizes derive from validated product settings and machine capability, with
safe compiled bounds. Jobs declare estimated pool cost in their registered
executor; Agent input cannot choose a pool, priority, retry count, or resource
limit.

Priority is `interactive > foreground > background`, but weighted aging and
per-Project fairness prevent starvation. An interactive request may preempt only
work whose executor declares safe cancellation/restart; it never steals a lease
or publishes partial output.

## Process isolation

External media/model executors launch with:

- an absolute payload-pinned executable and direct argv, never a shell;
- a minimal allowlisted environment with no UI session, CLI key, broker token,
  SourceGrant serialization, or Agent AppState;
- one API-owned per-attempt temporary directory;
- explicit input handles or product-resolved paths unavailable to command input;
- an OS process group on Unix or Job Object on Windows;
- bounded wall time, output bytes, child count, memory where supported, and log
  capture;
- network denied by policy except the dedicated ResourceJob downloader.

The Sequence renderer follows the same isolation contract as a dedicated
API-artifact helper. Its private execution manifest is compiler output, not a
business or Agent protocol: it contains only the pinned RenderPlan digest,
verified contained artifact/resource/tool paths and closure digests, exact
sampling schedules, output policy, and attempt-owned destination. Caller input
can never add argv, filters, paths, environment variables, or renderer options.

The renderer helper may start only the decoder/encoder tools named by its
verified capability closure. Those children expose bounded raw frame/PCM
streams; they do not own filter graphs, creative timestamps, scaling, mixing,
caption layout, or source selection. Adding another child tool requires a new
catalog closure and renderer identity.

Sequence render admission is part of the immutable private execution manifest.
The API uniquely derives peak active layers, pixel/audio work, decoder traversal
caps, byte ceilings, chunk sizes, scratch admission, and wall bound from the
validated RenderPlan/profile. The helper recomputes exact equality before it may
spawn a child. Callers cannot provide, increase, or omit a budget field.

Cancellation terminates the complete process tree after a bounded grace period.
Executor stdout/stderr is diagnostic input with size limits, never a product
protocol trusted without validation.

Process-tree containment is part of the generic lifecycle process primitive,
but is opt-in per process specification. On Unix it creates a dedicated process
group and terminates the group; on Windows it assigns the root to a Job Object
with kill-on-close semantics. Failure to establish requested containment fails
the process start before an executor may be considered running.

For the initial self-contained identify executor, the absolute executable is the
running signed API binary with a hidden internal argv mode. The source path is
sent only through bounded stdin, the environment is allowlisted, and output is
strict JSON containing no path. The helper has no child-process capability;
probe/decode executors still require the full process-group or Windows Job
Object policy before they can be registered.

## Publication and deduplication

Work writes only to the attempt temporary area. The API verifies expected media
facts and full digest, moves bytes atomically into a content-addressed or
job-owned canonical artifact area, then commits artifact row, logical job
success, selected result, and activity records in one SQLite transaction. A
losing or expired generation cannot publish even if its process later exits
successfully.

Equivalent jobs deduplicate by normalized executor kind/version, complete input
identity, parameters, and output schema. Sharing is explicit: cancelling one
owner removes its wait reference but cancels the logical job only when policy
allows and no owner remains.

A sequence-preview logical job additionally includes the exact Sequence
revision, dependency-resolver version, compiler version, output profile,
renderer build/target identity, and the fixed mapping from every input/resource
requirement to its producer job. `artifact-ready` prerequisites are satisfied
only while the selected immutable artifact is ready; a merely succeeded
producer whose bytes are evicted is not sufficient.

The Sequence preview job is created blocked when any selected proxy or font
result is not ready. Once all prerequisites are satisfied, its first attempt
publishes and binds one immutable RenderPlan before external rendering begins.
It never publishes a partial plan and never asks the Viewer to resubmit work to
advance the graph.

Sequence-preview claim requires an exact renderer-version match and revalidates
the canonical parameter envelope against typed input/resource rows. A producer
in `succeeded` state satisfies `artifact-ready` only while its selected typed
artifact is still `ready`; an evicted or missing result terminally fails that
logical preview so a later preparation can pin an explicit replacement job.

Resource downloads additionally verify the active release catalog entry before
publication. Export destinations are immutable project-owned artifacts; UI Save
As happens outside the render attempt.

## Cancellation and shutdown

Cancellation is a durable request. Queued work becomes cancelled immediately;
running work observes cancellation, terminates its tree, and records a terminal
attempt before the logical job becomes cancelled. Success that committed before
the cancellation transaction remains success.

On API shutdown the scheduler stops claiming, requests bounded worker quiescence,
and allows leases to expire if forced. B0, runner, and sidecars do not interpret
job state or delay lifecycle shutdown for business completion.

## Harness

- crash after every lease, heartbeat, output move, verification, and publication
  boundary and prove exactly one selected result;
- run a late expired generation and prove it cannot publish;
- verify pool limits, priority aging, and cross-Project fairness under load;
- cancel process trees on macOS, Windows, and Linux without leaving selected
  partial bytes;
- prove only ResourceJob has network authority and Agent input cannot alter
  executable, argv prefix, environment, destination, pool, or priority;
- restart with queued, running, publishing, blocked, and cancelled jobs while
  API readiness remains independent of queue drain.
