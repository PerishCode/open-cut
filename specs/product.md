# Agent-native product contract

Status: Business design baseline.

## Product thesis

Open Cut is a local, agent-native narrative video workspace. A creator can begin
with footage or writing and collaborate with an existing local agent to turn
intent into an explainable, reversible, professionally editable timeline.

Open Cut is not a chat panel attached to an editor. Chat is one intent surface
and the execution ledger is one project projection. Assets, narrative,
transcript, viewer, timeline, and export are equal projections of durable product
state.

"CapCut/Premiere-like" names the expected editing grammar: an asset library,
viewer, tracks, clips, captions, precise preview, and deterministic export. It
does not commit the first release to feature parity with either product.
Complexity is exposed progressively while the underlying edit model remains
precise.

## Primary user journey

The first vertical slice is footage-first:

1. The creator imports a local video by reference.
2. Open Cut records stable identity and metadata, then schedules proxy, frame,
   waveform, and transcript work independently.
3. The agent inspects available artifacts without waiting for every analysis job.
4. The agent explains what it observed and asks for missing creative intent.
5. The creator requests a paper edit, such as a vertical product demo.
6. The agent creates or edits a `PaperEdit`, then applies one atomic transaction
   derived from an explicit deterministic rough-cut preview that assembles the
   `main` sequence and its alignments.
7. The creator previews, manually edits, asks for further changes, or submits an
   inverse transaction.
8. Export pins one exact sequence revision and produces a verified local artifact
   under the contract in `specs/export.md`.

Later writing-first journeys reuse the same model:

```text
idea -> outline -> script -> visual intent -> asset work -> sequence
```

They do not introduce a second document or timeline truth.

## Actors and ownership

- The creator owns intent, approvals, manual edits, and final acceptance.
- The local agent owns reasoning, conversation, and selection of CLI commands.
- Open Cut owns projects, assets, analysis artifacts, narrative, sequences,
  alignments, edit transactions, ProductResources, business jobs, approvals, and
  exports.
- Cold start owns process activation and generic runtime topology only.
- The sidecar control plane owns process sessions and readiness only.

The agent is never a project truth source. Losing the agent conversation cannot
lose committed project state, and replaying a conversation cannot duplicate a
committed edit.

## Agent-native principle

The installed product CLI is the only Agent-facing entry. A prompt teaches the
agent to discover and execute the command tree through:

```text
<cli> <command> <subcommand> [--help]
```

MCP, SDKs, HTTP endpoints, sockets, sidecar capabilities, databases, project
files, and a second Open Cut Agent executable are forbidden Agent-facing
contracts. Any such mechanisms used inside the product remain CLI implementation
details.

## Product truth

A `Project` contains two peer creative models:

- `NarrativeDocument` describes authored structure and source selections.
- `Sequence` describes executable temporal composition.

Durable `Alignment` entities relate the two. Neither side silently rewrites the
other. Cross-model change is always an explicit `EditTransaction` with revision
preconditions and an inspectable result.

## Local-first behavior

- Original media is referenced in place by default and fingerprinted.
- Managed copies are explicit; proxies and analysis artifacts live below the
  API-owned app data directory.
- The local transcription engine ships with the payload; signed models are
  acquired on demand, retained as product resources, and then operate offline.
- Manual editing remains available if the local agent is unavailable.
- Agent disconnect does not cancel product jobs or roll back committed edits.
- Product restart resumes durable jobs and leaves waiting approvals visible.
- Cloud collaboration is not required for project correctness.

## Runtime feature availability

Core Project, Narrative, Sequence, and Edit truth remains available when an
optional media closure is absent or invalid. API readiness therefore describes
the migrated repository, application ports, scheduler recovery, and endpoint;
it does not claim that every derived-media feature is installed.

The active API derives one transient `open-cut/product-status/v1` snapshot from
its verified payload. The closed first feature set is asset frame inspection,
source preview, Sequence preview, and local transcription. Each feature is either `available` or
`unavailable` for exactly one of `not-installed`, `not-qualified`, or
`invalid-closure`. The snapshot is never persisted in SQLite and contains no
tool path, catalog identity, digest, sidecar state, or runtime-topology detail.
Web reads it through Contracts; the Agent reads the same semantics only through
`open-cut product status`.

An unavailable feature withholds its executor and prevents the Viewer from
blindly starting the corresponding preparation. Existing immutable jobs remain
blocked on their declared executor requirement; they are not rebound, mutated,
or failed merely because the active payload changed. Stable commands and routes
remain discoverable and return closed unavailable behavior.

Local transcription availability requires both the authenticated API-local
engine capability and a compatible entry in the active payload's authenticated
ProductResource catalog. It does not mean the model has already been acquired;
that installation-scoped state is a separate Creator-only resource snapshot.

## First-release editing surface

The first release supports the smallest deterministic editing vocabulary needed
for semantic rough cuts:

- multiple assets and one default `main` sequence;
- video, audio, and caption tracks;
- add, split, trim, move, remove, and reorder clip operations;
- aspect ratio and per-clip reframe state;
- transcript-backed paper edits and captions;
- transactional proposal, apply, conflict, history, and undo;
- preview and local export.

The first closed export target is the deterministic `webm-vp9-opus-v1` preset
defined in `specs/export.md`; horizontal and vertical delivery both use the
committed Sequence canvas rather than separate project formats. The reserved
`social-mp4-v1` preset becomes discoverable only in a release whose independent
H.264/AAC encoder capability and target-market distribution approval both pass.

## Explicit non-goals

The first vertical slice does not include:

- cloud collaboration or shared project mutation;
- generated video or third-party paid generation services;
- plugin ecosystems;
- arbitrary user-defined local Agent executable adapters;
- deep color grading, compositing, or a general effect graph;
- full Premiere feature parity;
- a general-purpose document editor;
- implicit script-to-timeline synchronization;
- old-schema or old-binary availability after forward migration.

## Acceptance statement

A clean installation is product-capable when a local agent, launched with the
shipped prompt, stable CLI on `PATH`, and scoped AppState defaults, can discover
commands, cold-start Open Cut, discover creator-authorized footage, inspect it,
wait for transcript work, create a paper edit, apply a reversible timeline
transaction, observe the result, and start a pinned export without learning any
internal endpoint or product data path.
