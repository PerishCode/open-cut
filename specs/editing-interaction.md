# Creator editing interaction and mutation budget

Status: Business design baseline.

## One creative commit path

Creator gestures and Agent proposals share the same normalized operation union,
precondition validation, canonical digest, journal, projection, activity, and
inverse construction. The UI may use a responsive one-call application use case,
but it does not own a second mutation model.

The first release permits only `creator` and `agent` creative actors. Background
jobs, schedulers, migrations, analysis, and product services cannot use a
`system` actor to modify creative state. A creator-approved Agent proposal keeps
the original Agent actor and records exact `approvedBy`, Approval ID, and
consumption provenance.

Project genesis is a creator transaction. Undo records the actor who requested
the inverse. Operational artifact selection never masquerades as a creative
transaction.

Creator editing and Agent editing have different authority edges but one
business kernel. The Agent can only create and apply proposals through the
stable product CLI. The first-party Creator UI calls a creator-only Contracts
write port backed by one API application use case; it never shells out to the
CLI and that route is never registered as an Agent command.

A Creator commit performs normalization, immutable Proposal construction,
ProposalApplication creation, Transaction construction, projection mutation,
inverse persistence, activity append, and request-identity persistence in one
SQLite transaction. There is no externally observable pending Creator proposal
and no partial success. A conflict or validation failure commits none of those
records. The returned Proposal and Transaction are the durable receipt.

Creator undo is another creator-only one-call application use case. It validates
that the target transaction's affected entities are still current, constructs
and commits the stored inverse as a new Proposal and Transaction in one SQLite
transaction, and retains `undoesTransactionId`. Undo-of-undo is redo. It does
not create an AgentRun, AgentTurn, command receipt, or command-registry leaf.

## Pointer and inspector gestures

During drag, trim, reframe, range selection, or property scrubbing, the UI holds
an ephemeral `EditDraft`:

```text
gestureId
base entity revisions
normalized candidate operations
preview overlay
validation issues
```

Pointer movement updates only the local overlay and preview input. It does not
write Project state, create hidden transactions, emit activity, or update
Contracts committed projections. Pointer cancel drops the draft.

Pointer-up or an explicit commit sends exactly one idempotent creator mutation
containing the complete operation set for the gesture. A multi-item move is one
transaction. Collision, stale preconditions, or invalid media semantics reject
the whole gesture. The UI preserves a rejected draft for inspection but never
silently rebases or changes intent.

Starting a creative gesture pauses playback and pins its base projection under
`specs/playback.md`. Selection and playhead movement alone are not creative
writes.

## Text editing and checkpointing

Narrative, caption, correction, and name inputs remain local drafts while the
creator is actively typing. The UI commits a checkpoint after 750 ms of idle,
on blur, on an explicit save/confirm gesture, and before a navigation that would
discard the editor.

Each checkpoint is one complete value replacement against the entity revision;
keystrokes are not individual transactions. A failed checkpoint remains visibly
unsaved and is not merged in the background. A crash may lose only the current
uncheckpointed interval, never a successful-looking committed edit.

The first release does not persist a second browser draft database. If durable
long-form drafts are later required, they need an explicit operational Draft
aggregate and recovery UX, not private creative truth in component storage.

The first direct-writing slice edits root Narrative `authored-text` children.
Content is plain UTF-8 complete-value replacement, purpose is explicitly
`spoken | on-screen`, and language is explicitly a canonical BCP-47 value with
`und` as the default unknown value. A new empty paragraph exists only as an
ephemeral editor row; its first non-empty checkpoint inserts the durable node.
An existing paragraph checkpoint updates the complete text value and requires
that node's exact revision.

Each draft permits at most one request in flight. If the Creator keeps typing
while it is in flight, success updates the committed base but does not overwrite
the newer draft; the newer value becomes the next checkpoint. A revision
conflict preserves the draft, labels it unsaved, and offers explicit reload or
retry-after-review. The UI never silently rebases or replaces the Creator's
text. Selecting a paragraph publishes the ordinary revisioned
`WorkspaceSelection`; focus alone never inserts an Agent composer attachment.

An ambiguous transport failure retries the byte-identical logical checkpoint,
including its request identity, base revision, preconditions, and operation
body. A known revision conflict is different: refresh-for-retry is an explicit
Creator action that adopts the current base while preserving the draft, then
creates a new logical checkpoint identity. The UI never mutates the body behind
an already accepted request identity.

Paragraph structure follows document-editor gestures without creating a second
write model:

- `Enter` splits a durable paragraph at the selection boundary; `Shift+Enter`
  inserts a plain-text line break inside that paragraph.
- an interior split is one transaction containing complete-value update of the
  left paragraph and insertion of the non-empty right paragraph. Both the
  paragraph and its parent carry exact preconditions.
- `Backspace` at offset zero merges only with the immediately preceding
  `authored-text` sibling when purpose and language match. The merge is one
  transaction containing complete-value update of the preceding paragraph and
  tombstone removal of the current paragraph. It never crosses a section,
  source excerpt, visual intent, note, or a mismatched authored-text role.
- boundary `Enter` moves the one ephemeral empty editor before or after the
  paragraph. An empty half is never persisted as a Narrative node, and a dirty
  ephemeral editor is never silently relocated.
- reorder is one `move-narrative-node` operation and remove is one
  `remove-narrative-node` operation, each with exact node and parent
  preconditions. Reorder is initially bounded to siblings present in the loaded
  Narrative page; cross-page movement waits for an explicit neighbor cursor
  contract.

Move and remove require the affected paragraph draft to be clean. A dirty
complete-value checkpoint must finish first; the UI does not combine two writes
to the same Narrative identity inside one proposal or discard an uncheckpointed
draft. Split and merge may consume the currently visible draft directly because
their complete resulting values are the transaction body. Structural conflicts
and ambiguous failures use the same explicit refresh versus byte-identical
retry rules as text checkpoints. Split, merge, move, remove, and their durable
Undo are reversible local creative effects, so they do not enter Approval.

## Section-aware long-form writing

Sections are explicit Narrative blocks, not syntax inferred from authored text.
Typing `#`, Markdown, or any other paragraph prefix never changes node kind.
A Creator creates a Section through an explicit UI gesture; its non-empty title
and canonical language are complete values, and new Sections inherit the parent
Section language unless the Creator explicitly changes it later.

Section titles use the same 750 ms/blur/explicit checkpoint rules and exact
retry semantics as paragraph text. `Enter` in a non-empty title checkpoints the
title, expands that Section, and focuses its one ephemeral child paragraph. The
empty child is not durable until its first non-empty checkpoint. Title editing
never rewrites child content, and paragraph Backspace never crosses a Section
boundary.

Expanding a Section performs one bounded `NarrativeSubtree` read against that
exact parent. Collapse is local presentation state and does not unload or mutate
creative truth. Each loaded Section owns one ephemeral paragraph insertion
anchor within its own sibling list; there is no document-global empty block that
can silently jump between parents.

Move and remove of a Section use the generic Narrative node operations. Move is
limited to exact siblings in the currently loaded page. Remove is enabled only
after the complete bounded child page proves the Section empty; a collapsed,
unloaded, partially paged, or non-empty Section cannot be removed from the UI.
The domain kernel remains authoritative and rejects non-empty removal even if a
caller bypasses that presentation guard.

Transcript selection and Narrative insertion remain separate controller state.
Choosing tokens does not change Agent `@` context, and focusing a Narrative node
does not select transcript evidence. `Insert excerpt` is one explicit Creator
gesture and one atomic transaction containing immutable `insert-source-excerpt`;
success returns the normal allocation/Transaction receipt and durable Undo.
Ambiguous delivery retries the identical selection, target anchor, revisions,
fingerprint, and request identity. Known conflict preserves the token selection
but requires the Creator to refresh/reselect exact evidence or target state.

## Creator rough-cut review

`Add to rough cut` creates an occurrence in an ephemeral ordered queue; it does
not check a set, create a Proposal, or mutate Narrative/Sequence state. The same
SourceExcerpt may be appended multiple times. Queue reorder/removal is local
presentation state and therefore has no Undo entry.

Every occurrence captures its exact SourceExcerpt revision and one explicit
video and/or audio Track/SourceStream binding. A unique compatible lane may be
prefilled visibly; ambiguity is a blocking choice. The draft also captures one
exact frame-grid Sequence playhead. It never infers an append position from the
last item in a bounded Sequence window.

Preview is a read-only Creator use case sharing the deterministic rough-cut
planner with the Agent CLI route. Any queue, binding, playhead, or durable read
change invalidates the old preview. The review itself is semantic rows and
timeline ghosts, not an uncommitted media render or durable browser draft.

Apply is one explicit gesture and one Creator transaction containing the exact
opaque preview operation. A 409 preserves the occurrence queue and requires a
new preview. An ambiguous failure keeps the exact apply envelope and request
identity for byte-identical retry. Success clears the draft, refreshes Sequence
and Narrative projections, and records one durable Undo receipt.

## Creator Timeline gestures

Timeline selection owns one primary Clip and, when present, exposes its complete
LinkGroup as a possible mutation scope. Arbitrary multi-select is not part of
the first slice. Selecting a linked Clip leaves scope unresolved until the
Creator explicitly chooses `linked` or `single`; the UI may recommend linked but
cannot inherit or default the request value invisibly. Selection alone does not
change Agent `@` context.

The gesture controller owns pointer/inspector draft, exact final state, chosen
scope and Alignment handling. It delegates playhead and pinned Sequence
revision to the existing headless Sequence Viewer controller; it never creates
a second playback clock or reads HTML media time. Pointer coordinates and
viewport scale may drive a local overlay, but the settled operation uses exact
frame-grid RationalTime values.

Move, trim, split and remove all use the server-side semantic planner in
`specs/timeline.md`, because a bounded UI projection is not complete authority
for linked members or Alignment dependencies. During plan/apply the overlay is
visibly committing. Conflict restores current committed projection and requires
a new gesture; ambiguous apply exposes only byte-identical retry. Collision
never causes implicit ripple, overwrite or neighbor movement.

After scope and Alignment handling are visibly selected, move/trim pointer-up
and split-tool click immediately plan and apply. Remove remains an explicit
command. Escape cancels only before the planner accepts the gesture; after that
point cancellation is represented by the same durable Undo path as every other
committed edit, with no confirmation modal or hidden compensating write.

Timeline viewport authority is headless controller state: exact origin, local
scale, scroll epoch and bounded loaded windows. A drag ghost is advisory and
edge dragging may request bounded adjacent pages and auto-scroll, but neither a
ghost nor the loaded window proves mutation closure. The final planner reads all
affected storage state. Pointer events from an obsolete viewport epoch cannot
silently reinterpret an existing draft.

## Creator direct source placement

`Place source` starts from one Source Viewer selection pinned to current Asset
revision/fingerprint and explicit video/audio SourceStreams. Source In/Out are
local exact absolute-source marks owned by the Source Viewer controller. Both
must be explicit unless the Creator invokes the visible `Use full selected
source` action, which computes the exact selected-lane coverage intersection.
Switching Asset revision, fingerprint or stream selection clears the marks.

The placement draft separately chooses an existing compatible video and/or
audio Track and explicitly captures the current Sequence Viewer playhead plus
its pinned Sequence revision. Source mode never reads a moving Sequence
playhead. Adoption of another revision makes the capture visibly stale and
requires a new capture. Unique lane pairs may be visibly prefilled; ambiguity
blocks commit. The Creator can use any non-empty subset of the pinned Source
Viewer lanes: one video lane, one audio lane or their linked pair. It cannot
implicitly add a Track, infer registration-default streams, append after a
bounded window, or borrow Transcript/Narrative selection.

`Place selected source at playhead` pauses both Viewer sessions, sends one
semantic request to the Creator-only planner and immediately applies its opaque
exact envelope. The planner fixes 1:1 duration, enabled Clips, optional durable
LinkGroup and complete collision closure. A 409 preserves marks/bindings but
requires a new plan; ambiguous apply exposes only identical retry. Success
adopts the returned Sequence revision, seeks its paused Viewer to the insertion
start, switches to Sequence mode, clears source marks/destination/lane draft but
retains the Source Viewer session, and publishes one receipt to Workspace
history. A failure while refreshing projection/history after a successful
commit is reported as post-commit degradation and never exposes mutation retry.

## Creator caption derivation

`Create captions` starts an ephemeral CaptionDraft from one exact
SourceExcerpt. The draft separately chooses one compatible committed Clip and
one Caption Track. A bounded visible Alignment can recommend a Clip, but only
the shared server planner proves the final SourceExcerpt/Clip/Track closure.
Choosing Caption inputs does not alter the primary Timeline gesture selection,
Narrative insertion anchor, Viewer playhead, or Agent `@` context.

Preview returns ordered semantic cue rows under expanded
`readable-captions-v1`; no cue is editable in-place and no durable identity is
reserved. Existing target-track overlap is a blocking insert-only conflict.
Apply commits every reviewed Caption and Alignment in one transaction, then
publishes its receipt to Workspace history. A 409 preserves the SourceExcerpt
orientation but invalidates the review and requires refreshed Clip/Track
selection. Ambiguous apply keeps only the identical opaque review/request for
retry.

## Creator manual Caption editing

Manual Caption creation, complete-value update, and removal are three semantic
Creator gestures over the existing Caption operation family. The Agent receives
no new surface and continues to use only the stable product CLI proposal path.
The Creator sends one exact Caption gesture to a Creator-only planner; the
planner reads the complete Track, collision, and exact dependent-Alignment
closure and returns a semantic review plus an opaque edit envelope owned by
Contracts.

A new manual Caption is an ephemeral draft until the Creator explicitly commits
one Caption Track, non-empty text, canonical language, and positive exact range.
The draft captures its start and end from the one Sequence Viewer playhead; it
does not own another clock or infer a hidden default duration. Creation records
`manual` provenance and never invents a SourceExcerpt, transcript citation, or
Narrative Alignment.

An existing Caption editor holds one complete local `{range, language, text}`
replacement. Text remains local while typing and checkpoints after 750 ms idle,
blur, or explicit save. An unchanged committed range is preserved byte-exactly;
only explicitly captured range boundaries move. A derived Caption retains its
derivation provenance, and any changed range, language, or text is projected as
`content: modified` rather than being relabeled manual.

Every exact dependent Caption Alignment is handled in the same gesture. A
timing-only change may use `preserve-if-provable` only when every Caption target
keeps the same identity and local range, text and language are unchanged, and
the final Caption duration still contains the target. Text or language changes
cannot preserve exactness. Remove cannot preserve. The Creator must explicitly
choose `mark-stale` or `unbind` whenever preservation is impossible.

Same-Track overlap rejects the complete gesture. There is no implicit ripple,
overwrite, trim, merge, split, or neighbor movement. The first slice edits one
Caption per gesture. A known conflict preserves the local draft but requires a
fresh plan; an ambiguous apply may only replay the identical opaque review and
request identity. Success publishes one receipt to Workspace history and never
creates a Caption-local Undo stack.

## Workspace creative history

Individual panes do not own competing Undo stacks. Every successful Creator
commit publishes its receipt to one Workspace history controller, while durable
transaction history reconstructs the same view after refresh or restart. The
history shows Creator and Agent actor provenance, intent, revision, changed
entities and undo lineage without exposing normalized operation wire bytes.

The default Undo action targets the visibly identified latest committed
transaction. Undo itself appends a new transaction; undoing that newest inverse
is Redo. A pane may retain transient retry state for its in-flight request, but
it cannot retain a separate durable-history truth or imply that another pane's
later transaction does not exist.

## Mutation budgets

The active command schema enforces these initial hard limits after UTF-8 decode
and before proposal creation:

```text
canonical normalized request: 1 MiB
operations per proposal: 512
proposal-local symbols: 1024
explicit preconditions: 2048
changed entity references: 2048
creator text in one field: 256 KiB
```

Operation-specific limits may be lower and are published by leaf CLI help.
Expansion during normalization must also fit the limits; a compact request
cannot create an unbounded implicit operation set.

An oversized proposal returns a typed budget issue with the measured category
and limit before ID allocation or mutation. The server never auto-splits a
proposal because doing so would change atomicity, Approval scope, retry identity,
revision conflicts, and undo.

The Agent divides genuinely independent work at stable Narrative section or
explicit creator-intent boundaries and commits each proposal separately. A goal
requiring all-or-nothing behavior must fit one transaction or use a future
explicit bulk domain primitive with its own bounded semantics—not hidden
chunking.

Impact classification is owned by a versioned active-payload business-kernel
classifier. It is not Agent input, a user preference, or a transport concern.
The classifier version and fully expanded result are part of the immutable
proposal digest. The initial authored-text, Caption, and Alignment operations
are reversible local effects and therefore do not require per-transaction
Approval after the CLI grant has the edit scope.

## Idempotency and UI concurrency

Every creator commit carries a gesture/checkpoint-derived request identity
scoped to the authenticated UI actor. That identity is stable for the logical
request across transport retry and is replaced only when the Creator creates a
new logical checkpoint. Retrying after a transport loss
returns the converged proposal/transaction outcome. Reusing the identity with a
different normalized candidate is invalid.

Concurrent windows use ordinary exact entity preconditions. A global Project
revision change does not reject an unrelated gesture, and no window owns a
mutable global active Project.

## Harness

- generate pointer streams and prove zero durable writes before commit and one
  transaction after pointer-up;
- cancel or conflict a multi-item gesture and prove no partial projection;
- test text idle/blur/navigation checkpoints, idempotent retry, and visible
  unsaved conflict state;
- reject each budget at its boundary before durable ID allocation;
- prove oversized requests never split and section-level Agent batches retain
  independent receipts and undo;
- scan journal actor provenance and reject any first-release creative `system`
  write.
