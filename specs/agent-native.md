# CLI-native Agent contract

Status: Business design baseline.

## Sole Agent-facing entry

The stable installed product CLI is the entire Agent-facing protocol. The local
agent receives a product prompt and the stable executable on its launch `PATH`,
conventionally:

```text
open-cut
```

The only discovery and execution grammar is:

```text
<cli> <command> <subcommand> [--help]
```

There is no Agent-facing MCP server, SDK, HTTP endpoint, socket, event stream,
database, project file, sidecar capability, or second Open Cut Agent entry
executable. Internal API or IPC transports may implement CLI behavior but are
not documented, emitted, or accepted as Agent dependencies.

The browser UI and internal application adapters are not constrained to use the
CLI. This prohibition is specifically the local agent boundary.

## Stable resolver

The installed `open-cut` resolver resolves `runtime.json.active` once per
invocation and dispatches to that payload's CLI. The active payload owns the
current command tree, help documents, input schemas, and result schemas. The
resolver does not translate old write commands, emulate removed commands, or
maintain cross-release write compatibility.

Help discovery does not start the product. Before dispatching a non-help business
command, the resolver asks the installed lifecycle host to ensure the active cell
and business API are ready, subject to a bounded readiness timeout. The
versioned CLI never starts sidecars, joins runtime topology, or obtains lifecycle
mutation authority itself.

Control-plane status remains observe-only. Creative commands use a separate
product business grant absorbed by the CLI implementation. The agent never sees
either credential or their internal transport.

## Invocation AppState

Each versioned CLI invocation constructs one typed immutable `AppState` before
command dispatch. It follows a Rust-like layered configuration model without
requiring the CLI implementation language to be Rust.

Configuration sources merge from least to most explicit:

```text
compiled defaults
< API-owned persisted non-secret invocation-policy snapshot
< launch-scoped parameters
< launch-scoped environment
< argv
```

Persisted settings own only bounded invocation policy such as interactive output
mode and wait duration. They never persist `projectId`, `sequenceId`, `runId`, or
`turnId`. For each business call the API returns one immutable settings snapshot
through the absorbed CLI authorization exchange; the snapshot revision and
effective policy are bound to that invocation. Environment and argv may narrow
or explicitly override those policy values within registry bounds. The Agent
bridge always forces machine JSON regardless of an interactive human preference.

The business context contains optional `projectId`, `sequenceId`, `runId`, and
`turnId`. The API Agent bridge creates or binds the run and active turn, then
injects safe context defaults such as `OPEN_CUT_PROJECT_ID`,
`OPEN_CUT_SEQUENCE_ID`, `OPEN_CUT_RUN_ID`, and `OPEN_CUT_TURN_ID`. It also
prepends the installed stable resolver directory to `PATH`.

This environment is a frozen launch context, not a mutable global "active
project." Concurrent windows and Agent runs receive independent values. An argv
flag may explicitly override a default, but selection never expands the caller's
business authorization.

Environment and settings must not contain a business grant, bearer token,
internal endpoint, receipt path, data directory, sidecar descriptor, or
authenticated actor identity. Actor identity comes from the absorbed business
grant, not a caller-controlled configuration value.

Every project-scoped result echoes the resolved context. A mutation additionally
verifies that its run belongs to the resolved project, its turn holds the active
generation write lease, every entity belongs to that project, and all revision
and request-identity preconditions hold. AppState defaults remove repetitive
flags; they never weaken validation or idempotency.

## Prompt contract

The shipped prompt is intentionally small. It identifies the CLI and requires
the agent to:

1. call the current command level with `--help` before using an unknown command;
2. never invent a command, flag, field, or result shape;
3. treat stdout JSON as authoritative and stderr as diagnostics only;
4. preserve returned project, entity, run, turn, job, approval, cursor, revision,
   and request identities across later calls;
5. use only the CLI even when another local interface appears discoverable.

The prompt does not duplicate the full command reference. Each `--help` response
is one versioned JSON discovery document containing the command path, children,
options, normative input schema, result statuses, mutability, approval behavior,
and examples. Complex EditTransaction schemas remain discoverable through the
leaf help document rather than a schema file or alternate protocol.

## Initial command tree

The first product command taxonomy is:

```text
open-cut product status
open-cut project list|show
open-cut activity list|wait
open-cut asset list|inspect|frames|analyze
open-cut transcript read
open-cut narrative show
open-cut sequence show|frames
open-cut edit derive-captions|derive-rough-cut|propose|apply|history|undo
open-cut run begin|resume|show|wait|complete|cancel
open-cut job show|wait|cancel
open-cut approval show|wait
open-cut export start|show|retry|cancel
```

Command names expose business intent, not storage or transport primitives. There
is no `sql`, `http`, `sidecar`, `broker`, `token`, `datadir`, or raw file-mutation
command.

`product status` reports only closed semantic feature availability and stable
unavailable reasons. It never exposes a media catalog, executable/resource
path, digest, release manifest, sidecar session, or control-plane capability.
The command tree remains stable when a feature is unavailable; help never hides
a command based on ambient machine state.

Rich read commands return compact summaries and stable IDs. The agent follows IDs
with narrower commands rather than requesting an unbounded project dump.
The bounded projections, page/window cursors, activity envelopes, and snapshot
reconciliation semantics are defined in `specs/read-activity.md`.

Before advertising creative writes, the same tree exposes bounded
read-before-write leaves for the relevant Narrative subtree, Sequence window,
individual entity, proposal, and transaction history. These reads return exact
revisions and stable anchors. An Agent is never expected to compensate with a
whole-Project blob, SQLite access, source-tree imports, or an internal transport.

## Input and output

Help and every successfully normalized command emit exactly one JSON document on
stdout. Machine JSON is the Agent launch default. Human-readable rendering may
be explicitly selected for interactive users, but the bridge must override a
persisted human preference so an Agent never scrapes tables.

Small inputs use flags. Typed edit transactions and other structured payloads use
stdin through an explicit CLI flag such as `--input -`; stdin remains part of the
single CLI invocation and is not another entry.

Within `edit propose`, new entities use bounded transaction-local symbolic IDs.
References are typed as existing durable ID or local symbol. The server assigns
durable identities during proposal creation and returns a stable mapping reused
by apply, approval, receipts, and idempotent retries. The Agent never invents a
durable entity ID.

Mutating Agent calls carry an explicit stable request identity. Retrying the same
request reuses the first accepted receipt and never executes a second logical
request. A command may separately project the current referenced object only
where its result schema explicitly defines that behavior; it cannot rewrite the
receipt's invocation-time status. Reusing the request identity with different
normalized input is rejected.

`edit propose` and `edit apply` are different logical requests and therefore use
different request identities. The proposal-creation identity converges on the
immutable proposal; the apply identity converges through direct commit or an
immutable `approval-required` result that references one Approval. Replaying the
apply identity returns that original result even after the Approval advances;
the Agent reads the final application outcome through `approval show|wait` and
the referenced Proposal or Transaction projection. A request identity never
changes its command or normalized input merely because a later command
references its result.

The CLI publishes the fixed proposal budgets from
`specs/editing-interaction.md`. Oversized input returns a typed invalid outcome;
neither CLI nor API silently splits a transaction. Independent Agent batches use
separate request identities and receipts.

stderr contains diagnostics and progress that may change between versions. No
Agent decision may depend on stderr text. Exit code `0` means a valid JSON help
or business envelope was produced, including `conflict`, `waiting`,
`approval-required`, `unavailable`, and terminal business failure. Exit code `2`
is an invocation or syntax error. Exit code `1` is reserved for a bootstrap or
transport failure that prevented a valid envelope from being produced.

## Result envelope

Every non-help command returns a versioned envelope of this logical form:

```text
schema
cliVersion
command
context
status
data?
error?
projectRevision?
activityCursor?
```

RationalTime numerators, revisions, activity cursors, and other 64-bit values
use the canonical decimal-string wire types in `specs/wire-contract.md`. The
Agent never has to preserve an unsafe JSON number.

Business statuses include at least:

- `succeeded`;
- `accepted` for durable asynchronous work;
- `conflict` with failed entity preconditions;
- `stale-turn` when the run's active AgentTurn generation has changed;
- `pairing-required` when the installation has no valid local business grant;
- `scope-upgrade-required` when the active grant lacks this command's current
  registry scope and a Creator-visible upgrade is pending;
- `approval-required` with an approval ID;
- `waiting` with a run or job ID;
- `not-found`;
- `unavailable` when the installed product cannot reach business readiness;
- `incompatible` when an active payload cannot continue an older run schema;
- `invalid` for a well-formed command with invalid business input;
- `failed` for terminal product failure.

For registry leaves classified `evidence` or `outcome`, these application
statuses are also the immutable command-receipt status once the invocation has
crossed current-Turn authorization and application validation. Pre-application
invocation and authority failures do not create receipts. Async and Approval
state is followed through the returned durable reference rather than by
mutating or completing the original receipt.

Errors are typed objects. Agents never parse localized message text to determine
control flow.

`context` contains the resolved project, sequence, run, and turn identities that
governed the command. It may report which configuration layer selected a value
for diagnosis, but never exposes the raw environment or persisted settings.

## Reads and creative writes

Read commands expose only business projections. Media frame results may contain
a bounded leased image resource materialized by the CLI. Its read-only path is a
time-limited command result for a generic Agent image reader; it is never an
original-media path, product data path, internal artifact path, or browsable
filesystem contract.

Caption derivation has one read-only planner: `edit derive-captions`
deterministically previews one exact SourceExcerpt-to-Clip operation without
reserving IDs or writing creative state.

Rough-cut materialization has one read-only planner: `edit derive-rough-cut`.
It accepts an explicit ordered SourceExcerpt selection, exact lane bindings, and
one destination start, then returns the complete deterministic Clip, LinkGroup,
and multi-target Alignment output defined in `specs/paper-edit-rough-cut.md`.

All Agent creative mutation is expressed through:

- `edit propose`: normalize, validate, digest, and persist an immutable proposal
  without changing creative state;
- `edit apply`: reference and atomically commit that exact proposal;
- `edit undo`: commit a new inverse transaction.

The first-party Creator UI may use a creator-only atomic edit commit/undo API,
but it is outside the Agent protocol: it is absent from CLI help and the command
registry and cannot be discovered, invoked, or depended on by the Agent. It
reuses the same normalization, Proposal, Transaction, inverse, projection, and
activity semantics; it is not a second creative mutation model.

`edit derive-captions` returns the fully expanded policy, preconditions, and
ordered local Caption/Alignment outputs. The Agent may only submit those bytes
through `edit propose --input -`; normalization re-derives and rejects tampering,
while apply commits the stored normalized operations without rerunning the
derivation.

`edit derive-rough-cut` follows the same planner/write boundary. It never
commits, schedules media work, chooses ambient streams, overwrites destination
content, or derives captions. The exact preview bytes enter the sole proposal
write and normalization rejects any divergent expansion.

A proposal request includes base project revision, exact entity revision
preconditions, typed operations with local references, intent, and request
identity. Its result includes proposal-assigned durable ID mappings. Apply uses a
separate request identity and includes proposal ID and digest. Conflicts are
returned without an automatic merge.

When apply returns `approval-required`, creator approval revalidates and executes
the exact immutable proposal. The Agent waits on durable state; it does not
resubmit or reconstruct the operation body. Replaying the original apply request
still returns the same `approval-required` receipt and Approval ID; it does not
append a synthetic committed receipt.

Proposal content state is `open | applied | stale | cancelled`. Each apply
request has a separate application state such as `approval-pending`, `committed`,
`denied`, `expired`, or `stale`. At most one application may await Approval at a
time. Denial or expiry leaves a still-valid proposal open for a later apply with
a new request identity; a failed exact revision precondition makes the proposal
permanently stale because revisions never move backward. `edit undo` atomically
creates an immutable inverse proposal and commits its forward transaction under
one new undo request identity.

## Durable work

CLI invocations are stateless transports over durable product work:

- the API bridge normally creates or binds a run before Agent launch and injects
  its project, run, and active turn IDs into AppState environment defaults;
- a standalone terminal Agent may instead use `run begin` and pass the returned
  run and turn through argv or its own subsequent environment;
- `run resume` explicitly and atomically supersedes the expected current turn and
  creates a new generation; it can recover an orphaned standalone turn without a
  hidden liveness TTL;
- subsequent Agent mutations resolve that run and active turn while reads may
  attach them as evidence;
- start commands return `runId`, `jobId`, `approvalId`, or `exportJobId`;
- `show` reads current durable state;
- `wait` performs a bounded wait from an activity cursor;
- disconnecting or terminating the CLI process does not cancel product work;
- `cancel` is an explicit command and does not undo already committed edits;
- `complete` explicitly declares the Agent intent complete and is rejected while
  the run has an unresolved blocker; neither presentation text nor edit count can
  imply completion;
- restarting Open Cut preserves status and resumes eligible jobs.

Open Cut resumes durable business state, not private model context. A later
Agent invocation reconstructs its working view from `run show`, referenced
entities, receipts, and current revisions.

There is no Agent-facing push channel. Any internal subscription is absorbed by a
bounded `wait` command and its JSON result.

## Pairing and approval

The CLI absorbs an OS-user-and-installation-scoped product business grant from
platform secure storage. A first creative command without a valid grant returns
a structured pairing requirement that the Open Cut UI presents to the creator.
The UI can revoke the pairing. The agent never receives, prints, or persists the
grant.

The grant is an installation Ed25519 key and API pairing record as defined in
`specs/local-authorization.md`. Request challenges and signatures remain hidden
transport details, never AppState or Agent prompt fields.

Read and media-inspection capabilities do not require per-command approval after
pairing. Reversible local edit transactions may apply automatically. Source
deletion, overwrite, paid or external invocation, and other high-impact actions
require durable approval.

## Forbidden behavior

- Help must not direct an agent to an internal endpoint or data path.
- The CLI must not print bearer credentials or sidecar descriptors.
- A command must not require the agent to inspect a project database or JSON file.
- The Agent prompt must not inject an internal schema unavailable through help.
- Agent fixtures must not bypass the command tree for speed.
- A hidden alternate Agent protocol is a contract violation even if the CLI also
  works.

## Compatibility

The stable resolver follows the active release, so CLI help and implementation
change atomically with product code and schema. Within a released version,
command help, result envelope, status names, and operation schemas are immutable.
Every envelope identifies that version. If an AgentRun crosses into an active
payload that cannot continue its command or receipt schema, the new CLI returns a
typed incompatibility and current durable run summary. The Agent rediscovers help
and starts or rebases a run; the resolver never silently dispatches an old write
implementation. Forward migration does not guarantee that an older active
payload can use newer project state.
