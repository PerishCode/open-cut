# Project versions

Project versions are Open Cut's low-intrusion disaster-recovery boundary. They
provide a Git-like mental model for creative project state without exposing Git,
SQLite, project files, or a second Agent-facing product interface.

## Product contract

- A version is an immutable, compressed canonical snapshot while retained.
- History is linear in v1. `parentVersionId` records ancestry but does not imply
  branch, merge, rebase, or cherry-pick behavior.
- Restoring never rewinds or deletes the current head. It atomically materializes
  the selected snapshot as one new Project revision.
- A restore first pins a `pre-restore` safety version of the current head in the
  same SQLite transaction. Failure rolls back the safety version, transaction,
  projection changes, request identity, and activity together.
- A restore journal entry carries one digest-bound `restore-project-version`
  reference. Entity expansion stays internal to the same transaction so large
  projects do not inflate the public EditTransaction payload.

## Capture boundaries

| Source | Boundary | Retention |
| --- | --- | --- |
| `genesis` | Project creation | pinned |
| `agent-turn` | Before a Creator starts or continues an Agent Turn | latest 32 unreferenced automatic versions |
| `manual` | Explicit named Creator action | manual; never automatically removed |
| `pre-restore` | Immediately before restore | pinned |

Automatic capture deduplicates `(Project, captured revision, source)` before
serializing state. Manual versions may intentionally name the same revision more
than once. Restored automatic versions are protected by their durable restore
request reference and are not eligible for automatic compaction.

## Snapshot closure

The snapshot owns canonical creative projection state:

- Project revision, PaperEdit document identity/revision, main Sequence
  identity/revision/format, and Track descriptors;
- Narrative nodes and typed payloads;
- transcript corrections and accepted Asset references/fingerprints;
- LinkGroups, Clips, Captions, and explicit Alignments.

It deliberately excludes source media bytes, proxies, models, resource and work
jobs, export artifacts/caches, physical paths, authorization material, runtime
topology, and UI sessions. Those remain independently durable or reproducible;
restored references continue to fail closed when required material is absent.

## Safety layers

Recovery does not make risky Agent behavior acceptable. Prompt version
`open-cut-agent-v2` requires exact reads before mutation, the narrowest operation
that satisfies the Creator request, preservation of unrelated content and durable
identities, explicit intent for destructive or broad effects, and receipt/state
verification after mutation. The stable `open-cut` CLI remains the Agent's sole
product capability, while hard-coded project versions provide the independent
last-resort fallback.

## Creator interaction

The Creator Workspace exposes versions in a dedicated panel, separate from the
read-only technical transaction log. It supports named saves, paged history, and
a two-step restore confirmation that explains both the new-revision behavior and
the automatic safety point. Per-transaction inverses remain internal edit
mechanics and are not presented as project-level Undo.

Future retention controls, pinning UI, branch labels, selective restore, and
content-addressed/delta storage may extend these fields. They must preserve the
same atomic restore, capability, and state-ownership boundaries.
