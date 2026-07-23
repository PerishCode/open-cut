# Local Agent bridge

Status: Business design baseline.

## Purpose and direction

The Agent bridge lets Open Cut invoke an existing local Agent while preserving
the CLI as the sole Agent-facing Open Cut protocol:

```text
Creator UI -> API AgentBridge -> local Agent process
                              -> open-cut CLI -> product business port
```

Open Cut may adapt how a supported Agent executable is launched and how its
presentation stream reaches the UI. It does not embed a model SDK into the
creative domain, translate Open Cut operations into an Agent-specific tool
protocol, or offer the Agent an API callback.

The bridge belongs to the API business/application layer. Its process adapter
does not import sidecar control concepts, start product peers, or acquire
lifecycle authority.

## Launch flow

For one creator request, the bridge:

1. resolves the selected Project, optional Sequence, bounded UI selection, and
   current revisions;
2. creates or binds one durable AgentRun for the creator intent and acquires a
   new AgentTurn generation; when no CLI grant has been paired yet, the run is
   explicitly `authorizing` and has no creative Agent actor;
3. builds the active payload's shipped prompt from fixed policy, creator intent,
   and a bounded product-ID selection summary;
4. asks the product-owned adapter resolver for one stable `open-cut`: installed
   modes validate the platform host's install receipt, while development
   materializes a private temporary resolver bound to the current product
   process; neither mode consults the API process `PATH`;
5. derives one private adapter home from the API datadir and Run ID, plus one
   per-turn scratch directory from the Run and Turn IDs; neither contains a
   project database, original media, data directory link, credential, or
   internal endpoint;
6. constructs an allowlisted child environment inside the adapter and sets its
   complete `PATH` to the stable resolver directory;
7. injects `OPEN_CUT_PROJECT_ID`, optional `OPEN_CUT_SEQUENCE_ID`,
   `OPEN_CUT_RUN_ID`, `OPEN_CUT_TURN_ID`, and machine-output defaults;
8. starts the configured Agent executable directly without a shell;
9. forwards its presentation stream to the UI while durable Open Cut receipts
   continue to come only from CLI calls.

The environment is fixed for that child process and inherited by its CLI
invocations. It is convenience context, not a credential or hidden tool schema.
Development resolver context remains a mode-private adapter implementation
detail: the Agent environment, prompt, help, and command results contain no
endpoint, signer path, receipt, or resolver path.

One accepted Creator composer submission creates exactly one durable AgentTurn
generation and at most one productive native turn. A resume probe may fall back
to a fresh native process only when the qualified protocol proves that no native
turn or tool execution started. Once any side effect may have occurred, failure
pauses the Run and requires an explicit later Creator submission. The first
submission creates the AgentRun; every later submission names the expected
current generation and atomically creates the next one. A run never queues a
second composer message behind an active turn.

When Codex emits a valid completed turn without the Agent explicitly completing
the Run, the AgentTurn becomes `completed` and the Run becomes
`paused: awaiting-creator`. An unexpected process or framing loss makes the turn
`detached` and the Run `paused: agent-detached`. A durable blocker may instead
leave the Run `waiting`. None of these states infer creative success from the
presentation stream.

An authorizing run associates the first proof-of-possession pairing request with
that run and turn. Creator approval does not impersonate the Agent. The first
subsequent successfully authorized CLI request atomically binds the approved
durable Agent principal and activates the run; before then every creative write
fails closed. A denied binding cannot silently fall back to the Creator actor.

The association is durable and grant-specific. A later turn of the same
authorizing Run may consume it after the original native turn has returned to
wait for Creator approval, but another Run or another grant cannot. If an active
installation grant already exists, the first successfully authorized command
performs the same binding without a new approval. In a bridge launch context,
Agent-facing `run begin`, `run resume`, and `run cancel` are rejected. The same
stable CLI permits safe `run show`, bounded `run wait`, and explicit `run
complete` for that bridge-owned Run. Those projections never contain adapter
paths, versions, or native session identity. Terminal `Cancel task` and creation
of a higher Turn generation remain Creator-only. Creator bridge transitions
never manufacture an Agent authority.

Pairing or Approval changes durable authorization or business state only. It
never launches another native process. An already-active Agent may observe the
change through a bounded CLI wait; otherwise the Run remains visibly
waiting/paused until the Creator submits Continue.

## Prompt boundary

The shipped policy prompt identifies `open-cut`, requires recursive `--help`
discovery, and forbids alternate Open Cut interfaces. It does not duplicate the
command tree or inject a private schema.

Creator intent and selected entities are untrusted content. They are delimited
from fixed policy, represented with product IDs and current revision summaries,
and cannot modify launch policy. Transcript, metadata, filenames, and Agent
output never become shell commands or environment variable names.

The Creator message text and its selection are separate fields. One accepted
Turn atomically snapshots at most 64 `ContextAttachment` values in a closed
union: a revisioned Asset/NarrativeNode/Clip/Caption/Track reference, an
immutable TranscriptArtifact+segment reference, or an exact revisioned Sequence
point/range. Project is Run-scoped; Sequence and attachments are Turn-scoped.
The canonical attachment summary is bounded to 16 KiB and enters the prompt as
orientation only. It contains no copied entity content, label supplied by the
Agent, DOM state, path, endpoint, credential, or application object. A later
entity change never rewrites the historical Turn.

Before mutation, the Agent must read current durable entities through the CLI;
the launch selection is orientation, not an edit precondition or truth snapshot.

## Agent adapters

The first release supports exactly one official OC Agent adapter plus standalone
use of the product CLI. Arbitrary executable templates, user-authored argument
interpolation, and generic Agent plugin loading are deferred until the official
adapter passes cancellation, recovery, security, and live-Agent qualification.

The OC adapter targets its supported headless/session contract. Each AgentTurn
records the resolved adapter version, Agent version, prompt version, and optional
opaque native session identity so qualification and recovery remain explainable.

A supported Agent adapter declares only Open Cut-to-Agent launch mechanics:

- executable resolution and fixed argument template;
- prompt delivery through stdin or a declared safe argument mode;
- working-directory and environment policy;
- presentation stdout/stderr framing;
- graceful cancellation and forced process-tree termination;
- optional native session continuation identity.

Adapter configuration cannot add an Open Cut tool other than the stable CLI,
point at an internal endpoint, or inject credentials. Arguments are built as an
argv array and never interpolated into a shell command.

Agent-specific conversation/session state remains owned by that Agent. An
optional continuation identity may be stored as opaque bridge metadata, but
Project correctness and AgentRun recovery cannot depend on it.

## First official adapter: Codex CLI

The first official adapter is `codex-cli-v1`. The implementation uses one
generic process-adapter engine driven by a shipped, read-only adapter
declaration, but the first-release registry contains only this declaration.
The declaration is product data, not a user-editable executable template.

The API resolves `codex` through a target-local `AgentLocator`. The locator may
check the API launch `PATH` and a closed set of official platform installation
locations, then invokes the candidate's version and login-status probes
directly. It never searches arbitrary directories, picks an unqualified binary,
persists the resolved path in Project state, or exposes it through Contracts,
activity, prompt, or CLI. Multiple candidates follow declaration order; a newer
ambient binary cannot displace an earlier qualified candidate merely because
its version sorts higher.

The prototype does not gate candidates by a declared Codex version range.
Version output is a bounded safe observation only; compatibility is decided by
the exact required prompt-input, JSONL, continuation, login, config/rule
isolation, and inline filesystem/network permission capabilities. A missing
executable, absent login, unrecognized protocol contract, or unavailable
security capability is a closed `missing | unauthenticated | incompatible`
adapter state. Open Cut never installs, upgrades, logs in, or silently falls
back to another Agent on the creator's behalf.

The Creator surface may read one safe availability projection containing only
the stable adapter ID, closed `available | missing | unauthenticated |
incompatible` state, prompt version, and observed adapter version when
available. Executable paths, candidate order, probe output, login material,
credential-store details, and private home paths never enter that projection.

For a new native session the adapter executes the qualified equivalent of
`codex exec --json` directly, supplies the complete Open Cut prompt on stdin,
and uses the per-turn scratch directory as the process working root. User Codex
configuration is not loaded; authentication remains owned by Codex's own
credential store. Open Cut does not select or persist an API key, provider,
model credential, MCP server, app, plugin, skill, hook, or arbitrary profile.
Web search is disabled; Agent-command network is limited to the local loopback
names required by the stable product CLI.

The host Codex process retains its existing `CODEX_HOME` solely so Codex's own
configured credential store remains authoritative. Every Turn passes
`--ignore-user-config` and `--ignore-rules`; Open Cut never reads, copies, or
projects `auth.json`, keyring material, user configuration, or user rules.
`CODEX_SQLITE_HOME` instead points to a private Run-scoped state directory below
the plain API datadir, so native continuation does not enter the user's Codex
state.

The private native state directory is disposable adapter cache, not Project
truth. It survives API restart while the active Project has a non-terminal Run
so exact native resume can work. It is removed after terminal Run transition and
when the Project is archived, tombstoned, or purged; the durable
ConversationLedger and business receipts remain. Missing, corrupt, or
already-collected native state therefore causes bounded fresh recovery rather
than Project damage.

The Codex command sandbox uses an Open Cut-owned inline least-privilege
permission profile: only platform-minimal runtime files and the stable resolver
directory are readable, and the current scratch root is writable. Network uses
Codex limited mode with only `127.0.0.1` and `localhost` allowed, no upstream
proxy, and no local binding. This lets the stable resolver and its active
payload absorb lifecycle discovery, signed CLI authorization, and dynamic
loopback transport. Direct API access remains unauthorized because the Agent
receives no endpoint, token, challenge, signature, or credential material.

That policy is supplied as fixed CLI overrides after user config and rules have
been disabled. Existing system or managed Codex policy may narrow or forbid it;
Open Cut never weakens a more restrictive machine policy. If the candidate
Codex release cannot ignore user config/rules or load the inline filesystem and
limited-network profile, the adapter is `incompatible`. Broad command network,
non-loopback domains, local listeners, `danger-full-access`, an approval bypass,
and an arbitrary shell allow rule are not fallback modes. The prototype treats
host-wide loopback visibility as explicit security debt; product authorization
still fails closed, and a future command-scoped transport can narrow this
without changing the Agent-facing CLI.

The child command environment seen by Codex tools contains only the stable
resolver-first `PATH`, the four scoped `OPEN_CUT_*` context values that apply,
and fixed machine-output policy. Codex authentication variables and home paths
needed by the Codex process itself are not forwarded to its tool commands.

The adapter parses a closed subset of Codex JSONL events. It captures
`thread.started.thread_id` as private opaque continuation metadata, streams only
agent-message text and safe typed activity/error states, and treats unknown or
malformed required events as an adapter failure. Raw reasoning, command text,
command output, file paths, environment, token usage, and stderr never become an
Open Cut receipt or durable creative evidence.

Native terminal JSONL is not itself the durable Turn terminal signal. The API
first commits `FinishAgentBridgeTurn`, then publishes the non-replayable
terminal presentation event and closes that stream. Creator reconciliation
therefore cannot observe the pre-finalized Run after presentation says the Turn
ended.

A later AgentTurn may attempt `codex exec resume` with the exact stored thread
identity, the new prompt on stdin, the new scratch working root, and the same
security policy. Qualification must prove that resume does not regain an older
scratch root. If the native thread is absent or incompatible before a native
turn or tool starts, the bridge may make one fresh attempt in the same Open Cut
Turn. Any ambiguous or post-start failure detaches instead of replaying. Fresh
recovery uses a deterministic bounded window containing the first Creator
intent, the newest completed Creator/Agent conversation messages, current
durable Run/activity receipts, and an explicit omitted-history marker. It never
reuses an older product turn's write lease.

Creator cancellation records the durable Run/Turn transition before signalling
Codex. The adapter then requests graceful termination and, after the common
bounded grace period, kills the contained Codex process tree. Native thread
survival cannot keep a cancelled or superseded Open Cut turn writable.

## Presentation versus receipts

Conversation text, partial model output, and generic Agent diagnostics are a
presentation stream. The UI may render them for the current session, but they do
not prove that an edit, job, approval, or export occurred.

The durable activity ledger reconciles independently from Open Cut command
receipts. If presentation claims success without a committed transaction receipt,
the UI shows no committed change. If presentation disconnects after commit, the
receipt and creative state remain visible.

The command registry classifies each leaf as `receipt: none | evidence |
outcome`. Help and observation polling are `none`; bounded product reads that
may inform a proposal are `evidence`; committed business commands are
`outcome`. A receipt stores only signed command identity, input digest and
request identity when present, safe input/result references, typed outcome,
revision/cursor evidence, and time. It never stores raw CLI output, prompt or
model text. Successful outcome receipts commit in the same SQLite transaction
as their business activity. A successful evidence read is not returned to the
Agent if its receipt cannot be persisted. Pre-business authentication,
transport, framing, or decoding failure is not a product receipt.

A receipt is the immutable result of the first accepted invocation of one
logical business request. Its status is the exact application result returned
at that boundary, including `succeeded`, `accepted`, `waiting`,
`approval-required`, `conflict`, `not-found`, `unavailable`, `incompatible`,
`invalid`, or `failed`. Linked Job, Approval, Export, Proposal, and Transaction
state evolves only in its own projection and activity; it never rewrites the
receipt or appends a synthetic terminal receipt. An idempotent request replay
reuses the original receipt.

CLI syntax, schema/canonicalization, pairing, scope, expired challenge, stale
Turn authority, transport, framing, and decoding failures occur before the
accepted application boundary and produce no product receipt. After that
boundary, every valid `evidence` or `outcome` business envelope requires a
receipt even when it reports a conflict or other result with no durable effect.
Any domain state transition and its receipt share one SQLite transaction. A
read or no-effect result is not returned unless its receipt has committed. Such
a receipt hashes the exact returned result, but does not claim that a completed
read transaction and the later receipt insert were one SQLite snapshot.
`projectRevision` and `activityCursor` are copied only when the original
business observation supplied them; the receipt layer never samples a newer
projection to manufacture snapshot evidence.

Conversation and receipts remain separate ledgers. Each has its own ordinal;
timestamps never manufacture a total order between them. The Creator groups
them by Run/Turn and reconciles both independently.

The bridge does not parse model reasoning to synthesize operations or infer
completion. Agent CLI calls remain explicit and independently validated.

Open Cut durably stores a paginated ConversationLedger for the non-purged
Project lifetime. Each entry and each recovery prompt is bounded. The ledger
contains exact Creator composer messages, completed `agent_message`
presentation items, and a closed safe `context-rebuilt` notice only. Partial
deltas, reasoning, command text/output, file changes, stderr, paths,
environment, usage, and arbitrary provider events are never persisted. A
Creator message is committed atomically with its AgentTurn before process
launch; replaying its request identity cannot create another turn.

When a native session cannot resume, the fresh prompt may use the bounded safe
conversation window plus current durable Run/activity receipts. Prior Agent
prose is delimited as untrusted presentation and is never a product fact, edit
precondition, or success claim. The vendor-neutral `context-rebuilt` notice is
durable Creator presentation so a stream gap cannot hide the reset; it is not
Agent input, a CLI receipt, or creative evidence.

The Creator-only live presentation stream is UI-session-bound and process-local.
It uses a turn-scoped monotonically increasing sequence only for gap detection;
it is not an Activity cursor and need not survive API restart. On a gap the Web
drops partial presentation, reloads the durable ConversationLedger and Run, and
reconnects. No presentation route, cursor, or message identity enters the CLI.

## Scratch and leased resources

The per-turn scratch directory is the only place where the API's injected
scratch adapter may materialize a bounded leased frame for the Agent's generic
image reader. The Agent reaches that operation only through the CLI; the CLI
cannot choose or override the physical destination. A lease is read-only,
randomly named, size-limited, digest-identified, and expires independently of
conversation state.

The scratch directory is not a project interchange format or hidden Open Cut
interface. The Agent cannot browse from it into source media or product data.
Once a turn is terminal and all leases expire, product collection may remove it.

Files created independently by the Agent in scratch are untrusted working files
and never enter Project state unless the creator authorizes an explicit import
through a normal product surface.

## Pairing and authorization

The Agent child receives no business token. The stable CLI authenticates through
the OS-user-and-installation pairing held in platform secure storage. AppState
project and run IDs select context but do not authorize it.

High-impact action approval remains creator-facing durable product state. The
bridge can notify the UI that a run is waiting, but cannot approve on the
creator's behalf or convert presentation text into approval.

## Process loss and cancellation

The Agent process, AgentTurn, and AgentRun are related but distinct state
machines. One run has at most one active writer turn. Killing a process does not
undo transactions, cancel shared MediaJobs, or erase receipts. Product restart
reconstructs the run from durable state even if an Agent-native session cannot
be resumed.

An unexpected process exit detaches its AgentTurn. An otherwise active run moves
to `paused: agent-detached`; a run already waiting on durable work remains
waiting. Resume names the expected generation, atomically creates the higher
generation, and supersedes the old write lease even when an orphaned standalone
turn still says `active`. CLI writes carrying an older turn are rejected. No
heartbeat or inactivity TTL is a correctness oracle for Agent process liveness.

`Stop` and `Cancel task` are distinct Creator operations. Stop first marks only
the active turn `cancelled` and the non-terminal Run `paused:
creator-interrupted`; a later composer submission creates a higher generation.
Cancel task first makes the Run terminal `cancelled`. Both then request graceful
Agent termination and finally terminate the owned process tree after a bounded
grace period. Late CLI mutations from a cancelled or superseded turn are
rejected.

The first budgets are measured after UTF-8 decode: 32 KiB per Creator message,
256 KiB of completed Agent message text per turn, 256 KiB per JSONL record, and
1 MiB of queued live presentation per subscriber. One native turn has a
30-minute wall-clock budget. Exceeding a budget terminates that invocation and
pauses the Run with typed `resource-limit`; it never rolls back committed work.

## Invariants

- The local Agent can reach Open Cut only through the stable product CLI.
- The bridge never supplies payload-private paths, endpoints, tokens, databases,
  sidecar state, or operation schemas.
- One Agent adapter cannot change product command or transaction semantics.
- One AgentRun cannot have two concurrent writer turns.
- Presentation is never used as a creative or job success oracle.
- Agent process lifetime never owns committed edits or shared job lifetime.
- Scratch files never become implicit Project inputs or truth.
- One accepted Creator composer request creates exactly one AgentTurn.
- One AgentTurn has at most one native attempt that may have started tools.
- Conversation history contains no reasoning, command transcript, or success oracle.
- Native session identity and resolved executable path never enter Creator or Agent projections.
