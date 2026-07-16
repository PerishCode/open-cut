# Paper edit to rough cut

Status: Business design baseline.

## Purpose

The first materializer turns an explicit ordered selection of exact
`source-excerpt` Narrative nodes into reviewable Sequence operations. It is a
deterministic planning aid, not a second creative writer and not background
synchronization between PaperEdit and Sequence.

The Agent-facing read is:

```text
open-cut edit derive-rough-cut --input -
```

It is discovered through the ordinary CLI help tree. The only structured
creative proposal write remains `edit propose --input -`, and only `edit apply`
commits the resulting immutable proposal.

The derivation command reads one strict JSON planning query from stdin because
the ordered selection may contain 128 independently pinned lane bindings. This
does not create another write channel: the command is registry-classified
`read-only`, its API POST is only a body-carrying query, and it performs no
durable or operational mutation. Argv, stdin, and the returned JSON remain
reachable only through the stable product CLI; the Agent never receives an
HTTP, repository, or sidecar entry.

## Exact input

One request names:

- the exact Project and destination Sequence from AppState;
- an explicit non-empty ordered list of existing SourceExcerpt IDs and entity
  revisions;
- one explicit destination timeline start;
- one explicit video Track and SourceStream binding and/or one explicit audio
  Track and SourceStream binding for every excerpt;
- a proposal-local output prefix.

Every selected SourceExcerpt and Track carries its exact revision in the
planning query. The preview returns the deduplicated Asset, Sequence,
SourceExcerpt, Track, and TranscriptCorrection preconditions needed by the
proposal. If those preconditions exceed the global proposal budget, the whole
query is rejected rather than silently weakening evidence closure.

The list, rather than an ambient subtree traversal, is the selected paper edit.
Sections, authored text, visual intent, and notes remain Narrative truth and are
not silently converted into media. The caller may select all SourceExcerpts in
one section, but the planner never guesses which non-media intent should become
footage.

The same SourceExcerpt may appear more than once. Repetition is an explicit
editorial choice: every occurrence has its own ordered lane binding and
proposal-local output identities. The planner never deduplicates repeated
footage or treats it as an accidental request.

The Creator UI represents this list as a controller-owned ephemeral occurrence
queue, not a set of checked Narrative identities. `Add to rough cut` appends one
occurrence; reorder and remove change only that queue, and a second addition of
the same SourceExcerpt is a deliberate repeat. Project navigation or successful
apply clears the queue. Ordinary read reconciliation may retain it, but always
invalidates an older preview.

Every stream and track is named. Probe defaults, selected Viewer streams,
current UI selection, track array positions, and runtime locale are never
materialization inputs. At least one compatible video or audio lane is required
per excerpt. When both lanes are present, they refer to the same Asset and exact
source range.

The Creator controller may visibly prefill a lane only when exactly one
compatible Track/SourceStream pair exists for that media kind. The resulting
IDs and Track revision remain explicit review input. Multiple or missing
candidates block preview until the Creator chooses a concrete pair; no hidden
probe/default-stream preference resolves ambiguity.

The destination start is a controller-owned exact Sequence playhead snapped to
the Sequence frame grid. It never comes from an HTML media element float or a
rounded display value. Until richer timeline navigation is present, an empty
Sequence may initialize the visible exact start to zero; a non-empty Sequence
without an exact playhead selection cannot guess its tail from a bounded page.

## Deterministic layout

`paper-edit-rough-cut-v1` has one immutable layout policy:

```text
ordering                 request excerpt order
first timeline start     explicit request value
inter-excerpt gap        zero
source handles           zero
rate                     1:1
overwrite/ripple         forbidden
A/V grouping             one LinkGroup per two-lane excerpt
```

Each excerpt retains its exact source duration. Its first generated Clip starts
at the current timeline cursor; the next excerpt begins at the exact end. A
single-lane excerpt creates one Clip without a LinkGroup. A two-lane excerpt
creates one video Clip and one audio Clip with equal timeline ranges and one new
LinkGroup. Source bounds, stream compatibility, exact arithmetic, and
non-overlap with committed destination contents are validated before output.

The planner never clamps an excerpt, fills a missing lane, inserts handles,
chooses B-roll, changes playback rate, closes or creates gaps outside its exact
output, removes existing Sequence state, or moves neighboring Clips. Any
unsatisfied input fails the whole preview with indexed typed issues.

## Preview and proposal closure

The read-only preview returns:

- base Project revision and complete entity preconditions;
- the fully expanded immutable policy;
- the exact ordered excerpt and lane bindings;
- proposal-local LinkGroup, Clip, and Alignment symbols;
- every final source and timeline range;
- the explicit primitive output tuples and a canonical output digest.

The preview reserves no durable IDs and writes no creative or operational state.
The Agent submits one `derive-rough-cut` operation containing those exact bytes
through `edit propose --input -`. Proposal normalization recomputes the complete
primitive output and rejects missing, reordered, modified, or injected results.
The normalized proposal stores explicit `put-link-group`, `put-clip`, and
`put-alignment` operations. Apply commits those stored bytes and never reruns the
materializer.

The first-party Creator uses a separate authority edge and Contracts port over
the same application/repository planner. It does not call the Agent command
route, manufacture a command receipt, or expose another Agent entry. Contracts
normalizes the response into a stable semantic review and retains the exact
wire operation in an opaque in-memory envelope. Web cannot construct or edit
that envelope.

Creator review is ephemeral: it does not persist a Draft or externally visible
pending Proposal. It shows ordered occurrences, effective Narrative text,
source/timeline ranges, concrete lanes, generated Clip/LinkGroup/Alignment
shape, and the output digest. Timeline ghost blocks are presentation only. It
does not schedule an uncommitted audiovisual render; playback is prepared only
after committed Sequence state exists.

One explicit Creator Apply sends the reviewed envelope through the existing
one-call Creator commit kernel. Normalization recomputes the derivation exactly,
then atomically persists one Proposal and Transaction. An ambiguous delivery
may replay only the byte-identical apply request. A known conflict preserves the
occurrence queue, invalidates the preview, and requires a new preview; success
clears the queue and exposes the ordinary durable Undo.

One planner operation accepts at most 128 SourceExcerpts and its expansion must
fit the global proposal budgets. It is never auto-split because splitting would
change atomicity, review, approval, conflict, and undo boundaries.

## Alignment and later editing

One SourceExcerpt creates one Alignment whose target set contains the full local
range of every generated Clip for that excerpt. A two-lane A/V realization is
therefore one semantic alignment with two Clip targets, not two unrelated
alignments. The exact Narrative revision, Clip revisions, local ranges, and
deriving transaction are durable evidence.

Clip does not gain a parallel derivation-provenance model. The normalized
EditTransaction records origin, while Alignment records current semantic
correspondence. A later move may keep the Alignment exact only when the same
transaction includes a complete `remap-alignment` whose target revisions and
local ranges prove the same source mapping. Trim and split may remain exact only
under the semantic-coverage proof in `specs/timeline.md`; replacement, unlinking,
or removal must explicitly remap the complete target set or mark the Alignment
stale or unbound.

Re-running the planner is always a new explicit proposal. It never repairs or
replaces an earlier realization in the background.

## Captions and other derived work

Rough-cut materialization does not derive captions, schedule media work, prepare
preview, or start export. Captions use the independently versioned policy in
`specs/transcript-caption.md` after stable Clips exist. Operational preparation
continues through the existing job and Viewer contracts.

This separation keeps the planner a pure creative projection and ensures that
missing optional executors do not change its normalized timeline semantics.

## Harness

- snapshot identical outputs and digest for identical exact inputs;
- reject stale excerpt, track, Asset, stream, or Sequence preconditions;
- reject any output tampering and prove preview performs no durable write;
- cover video-only, audio-only, and linked A/V excerpts;
- prove zero-gap exact packing, no implicit overwrite/ripple, and whole-request
  rejection on one invalid excerpt;
- apply, retry, undo, and redo while preserving one multi-target Alignment per
  SourceExcerpt;
- move one linked realization while preserving exact alignment, then trim or
  remove it and require explicit remap, stale, or unbound state;
- derive captions only in a subsequent explicit proposal against committed Clip
  identities.
