# Creator workspace projection

Status: Business design baseline.

## Product surface

The desktop workspace is a writing-aware nonlinear editor with four persistent
regions:

```text
+------------------+--------------------------+---------------------------+
| Intent / Agent   | Assets / Narrative /     | Viewer / Inspector        |
|                  | Transcript               |                           |
+------------------+--------------------------+---------------------------+
|                         Timeline                                         |
+---------------------------------------------------------------------------+
```

Panels may resize, collapse, or temporarily take focus, but the underlying
product model does not change with layout. The first release is desktop-first
and preserves usable viewer and timeline space before exposing secondary
inspectors.

The workspace does not imitate every Premiere panel. It keeps familiar editing
grammar while making authored intent, source evidence, and transaction history
first-class.

## Shared selection context

The UI maintains one revisioned workspace selection containing product IDs only:

- project and sequence;
- asset or transcript segments;
- narrative nodes;
- timeline clips, caption items, and tracks;
- playhead and exact selected ranges;
- transaction, run, job, export, or approval.

Selection is ephemeral UI state, not creative truth. When the creator invokes
the local Agent, the API Agent bridge creates or binds an AgentRun and AgentTurn,
serializes a bounded selection summary into the prompt, places the stable CLI on
`PATH`, and injects launch-scoped project, sequence, run, and turn defaults. The
Agent still re-reads current durable entities through CLI commands before
mutating them.

One controller-owned `WorkspaceSelection` closed union is the only selection
truth shared by Assets, Transcript, Narrative, Timeline, Viewer, and the Agent
composer. Panes publish typed selection intents into that controller; they do
not expose DOM nodes, private component state, or parallel selection stores to
the Agent pane. Selection remains ephemeral even when an exact revisioned copy
is accepted as a Turn attachment.

The composer renders `@` references as chips but submits typed
`ContextAttachment` values separately from human message text. Accepted
attachments are immutable Turn history; removing a chip before submission does
not mutate product state, and changing workspace selection after submission
does not alter the running Turn. Bulk timeline orientation uses one exact range
attachment rather than enumerating every intersecting Clip.

Submission validates all selected references against one API transaction. A
stale attachment rejects the submission without clearing the Creator message or
chips. Candidate discovery never scrapes rendered labels or derives IDs from
routes.

No selected file path, credential, internal endpoint, or private application
object enters the prompt.

## Intent and Agent pane

The intent pane combines the local Agent session surface with Open Cut's durable
activity projection. These are visually related but not conflated:

- conversation and reasoning belong to the local Agent session;
- runs, command receipts, proposals, approvals, transactions, jobs, and exports
  are rendered from Open Cut durable state;
- losing conversation history does not remove committed work;
- a receipt card links to affected narrative or timeline entities by ID and
  revision.

Within one Turn, conversation and receipt cards occupy visibly distinct lanes.
Conversation ordinal orders durable messages and receipt ordinal orders command
evidence; display timestamps may be shown but never interleave the two as a
claimed causal total order.

The Run surface reads an authoritative creator-only `AgentTurnPage`. Historical
Turns are ordered only by their Run generation, and the UI lazy-loads each
expanded Turn's `TurnReceiptPage`. There is no run-wide receipt cursor or total
order synthesized from timestamps.

The Agent bridge presentation stream can disappear or reconnect independently
of the activity ledger. A successful-looking chat message without a product
receipt never changes the committed canvas.

The composer accepts at most one in-flight submission. Each accepted message is
shown immediately from the durable ConversationLedger and owns one AgentTurn.
Completed Agent messages are durable presentation history; partial streaming
text is visibly transient. After a stream gap or restart, the pane discards the
partial text and reconciles history, Run state, and activity independently.

`Stop` interrupts the current turn and leaves the task resumable. `Cancel task`
is a separate terminal action. The pane never exposes native session IDs,
executable paths, raw commands, command output, reasoning, stderr, environment,
or token/account credentials. Agent text renders without raw HTML, remote image
loading, or automatic external navigation.

The pane has an explicit Run selector and `New task` action. Normal composer
submission continues the selected non-terminal Run; `New task` creates an
independent Run and never cancels an older paused Run. Multiple windows or Runs
may work on one Project and resolve normal revision conflicts, but each Run has
only one active Turn. A selected terminal Run remains readable and requires an
explicit new-task state before another message can start work.

Before submission the pane renders the safe local adapter availability as a
closed product state. Missing, unauthenticated, or incompatible states disable
submission and explain the required external action without offering to install,
upgrade, log in, copy credentials, or reveal a local path. Existing Run history
remains readable while the adapter is unavailable.

Pairing and Approval controls never auto-submit the composer or create a native
Turn. If the existing process has already returned, the pane shows the durable
waiting/paused state and the Creator explicitly continues. A durable
`context-rebuilt` notice explains loss of native continuity without exposing the
adapter, native session, filesystem, or credential state.

The creator can inspect a proposed diff, jump to affected ranges, cancel a run,
answer an approval, or commit an inverse undo. The pane never displays hidden
credentials or treats free-form model text as an action result.

Receipt references dispatch a typed `WorkspaceFocusIntent` through the same
workspace controller. Focus is ephemeral navigation: it does not select a Run,
submit an attachment, or mutate creative state. If the referenced revision is
not the current revision, the UI labels the historical/current mismatch and
never presents the current canvas as the receipt-time snapshot. Tombstoned or
unavailable references remain readable as durable receipt evidence.

An unexpected Agent detach leaves the run visibly paused and resumable. Starting
a new turn supersedes the old write lease; it does not duplicate the run or hide
already committed receipts.

## Assets, transcript, and narrative

The upper editing region exposes three peer projections:

- Assets shows registered sources, availability, facts, artifact readiness, and
  creator-only import or relink actions.
- Transcript shows immutable recognition segments aligned to source time plus
  separate authored corrections.
- Narrative shows the restricted PaperEdit tree and its exact, stale, or unbound
  timeline alignments.

Selecting a transcript range can create a `source-excerpt` through a normal
EditTransaction. Reordering or editing PaperEdit nodes does not alter the
timeline until an explicit transaction also contains sequence operations.

The initial Narrative writer renders root authored-text children as plain-text
paragraph editors plus one ephemeral empty paragraph. It shows committed,
saving, unsaved, and conflict states without maintaining a private durable copy.
Idle, blur, explicit save, or navigation checkpoints use the creator-only
Contracts edit port and the shared edit transaction kernel. A successful
checkpoint exposes an explicit durable Undo action for its Transaction; a
conflict keeps the local text visible and never silently refreshes it away.

`Enter` splits at the caret, while `Shift+Enter` inserts a line break.
`Backspace` at the start merges only adjacent compatible authored-text
paragraphs. Empty split halves reposition the single ephemeral paragraph rather
than creating durable empty nodes. Reorder and remove are explicit controls and
operate only on clean drafts; every split, merge, reorder, or remove is one
atomic Creator edit with durable Undo. The first bounded writer reorders only
within the loaded sibling page and never guesses an off-page neighbor.

Long-form structure uses explicit Section blocks. The writer does not interpret
Markdown headings. A Section title is a single-line checkpointed field with an
expand/collapse control; `Enter` saves a non-empty title, expands its bounded
child page, and places the Creator in the Section's ephemeral first paragraph.
Each expanded branch has its own paragraph insertion anchor and inherits the
Section language for new authored text.

Collapsed or unloaded branches remain visible as Section summaries but provide
no guessed child operations. Section removal appears only when a fully loaded
page proves zero children and no continuation; reorder remains bounded to the
visible sibling page. Nested reads and writes continue through Contracts and the
same Creator edit port rather than introducing a tree-editor transport.

Transcript tokens expose an explicit contiguous SourceExcerpt selection separate
from `Use as @ context`. The UI shows the selected exact time interval and
effective correction closure, then offers `Insert excerpt` at the active
Narrative anchor. It never copies transcript display text into an authored-text
paragraph. Partial correction overlap, stale evidence, missing accepted
fingerprint, or absent target authority is shown as a blocking typed state.

The active insertion anchor is visible as root end or after the last focused
Narrative node. After commit, selection may clear and the anchor advances to the
new SourceExcerpt allocation; Undo remains the ordinary Narrative transaction
action. Token selection itself is ephemeral and never creates a Proposal,
activity event, Agent attachment, or browser draft database.

Every exact SourceExcerpt row also offers `Add to rough cut`. The resulting
draft is an occurrence queue: repeated additions are preserved, and explicit
move/remove controls define request order. It is separate from Narrative
selection, transcript selection, timeline clip selection, and Agent `@`
attachments.

The rough-cut inspector shows the exact destination playhead and each
occurrence's concrete video/audio Track and SourceStream. A media kind with one
compatible pair is visibly prefilled; multiple candidates render explicit
choices and block preview until resolved. Missing or stale SourceExcerpt
evidence, an invalid playhead, or any unbound occurrence is visible as a typed
blocker.

`Preview rough cut` produces semantic review rows and timeline ghost blocks
from the shared deterministic planner. No uncommitted media job or creative
state is created. `Apply reviewed rough cut` commits the exact reviewed envelope
as one Creator transaction; conflict returns to preview-required state while
preserving the occurrence queue, and success clears it and refreshes the
committed Sequence Viewer.

Paragraph selection flows through the same controller-owned
`WorkspaceSelection` used by the rest of the workspace. It can later be chosen
as an explicit `@` attachment, but selection or editing never auto-submits or
auto-attaches it to the Agent pane.

## Viewer

The Viewer has two explicit modes:

- source mode previews one asset at exact source time;
- sequence mode renders the selected committed Sequence revision.

Opening a Project starts in Sequence mode. Asset selection alone does not
replace the program view; `Open in Source Viewer` is an explicit command.
Returning to Sequence mode restores its pinned session and playhead. Source and
Sequence modes may retain separate ephemeral selections, but neither may mutate
the other's continuation or silently adopt creative state.

The header always identifies the mode and revision. Proxy use is an implementation
optimization; degraded or missing media is visible and never silently replaced.

Contracts gives the Viewer only a purpose-bound same-origin MediaLease. The
`oc://`/Web proxy chain injects the renderer-unreadable first-party UI session,
and the API serves validated Range requests without exposing source or artifact
paths. Lease and renewal behavior are defined in `specs/media-delivery.md`.

Source mode owns a headless Source Viewer controller separate from the Sequence
controller. It displays the pinned Asset revision/fingerprint and explicit
video/audio SourceStream selection, exposes exact absolute-source playhead plus
In/Out marks, and treats the media element as an actuator. Stream ambiguity is
visible; changing the pinned source invalidates marks. Browser rounded time is
never shown as a settled creative coordinate.

Crop and reframe overlays edit inspectable clip state. Preview gestures generate
the same typed operations and transaction path as timeline or Agent edits.
Pointer and text drafts remain visibly ephemeral until the checkpoint rules in
`specs/editing-interaction.md` create one idempotent transaction.

Playback pins one Sequence revision and RenderPlan, uses the exact master-clock
rules in `specs/playback.md`, and pauses before a creative gesture. A newer
revision is reconciled explicitly rather than hot-swapped beneath playback.
Sequence mode keeps one always-visible transport over the program picture:
current/semantic timecode, start, previous frame, play/pause, and next frame.
The native media element is an actuator, not a second hidden transport. A
Timeline seek immediately actuates that same media session; playback clock
observations are settled onto the pinned Sequence frame grid before updating
the shared logical playhead.

## Timeline

The timeline projects typed video, audio, and caption tracks with exact playhead,
clip, trim, selection, and alignment state. Manual edits are transaction-backed
and appear in the same history as Agent edits.

The first release exposes selection, move, trim, split, remove, track ordering,
captions, format, and reframe. Ripple behavior is never inferred from cursor
mode; a creator-visible command must produce the complete explicit operation set.

Caption items visibly distinguish manual from transcript-derived origin. A
derived item shows its independent content and evidence status, including the
valid state where creator-polished content is `modified` while its cited source
evidence is still `exact`, or where retained content outlives `stale` evidence.

Narrative alignment indicators are overlays. They explain correspondence but do
not make Narrative the playback source.

Clip/Caption-anchored indicators follow their entities when an exact transaction
preserves local semantic range. Raw timeline-range indicators are visually
distinct because any Sequence change may make them stale.

The first interaction slice selects exactly one primary Clip. A linked Clip
requires an explicit `linked | single` scope choice before a mutation; linked
may be visually recommended but is never silently supplied. The selected Clip,
scope and active gesture are Timeline controller state, separate from the Agent
composer. `Use selected Clip as @ context` remains an explicit action.

Move, trim, split and remove produce local exact-time ghosts, then use the
Creator semantic gesture planner to close complete LinkGroup, collision and
Alignment dependencies. Preserve, stale and unbind outcomes are shown before
apply. The planner's exact edit envelope remains opaque to Web. Ripple and
overwrite have no implicit pointer mode in this slice.

After commit, move and trim keep the same selected Clip identity at its returned
revision. Split selects the seed Clip's right output through a Contracts-owned
safe selection hint; remove clears Timeline selection. All four preserve the
exact gesture playhead and adopt the returned Sequence revision. The hint is
usable only after the refreshed projection proves the hinted identity and
revision; projection refresh failure is feedback failure after a successful
mutation and never becomes an apply retry.

Timeline does not own another playhead. It reads and commands the same headless
Sequence Viewer controller used by playback, so play, seek, frame-step,
rough-cut destination and edit gestures share one exact logical position.

## Proposal and commit feedback

Before a creative commit, the UI can render the normalized proposal as:

- human intent summary;
- affected entity and revision set;
- narrative insert, update, move, or tombstone diff;
- timeline clip, caption, format, and reframe diff;
- alignment changes and expected stale relationships;
- protected effects and approval requirement.

After commit, the proposal card becomes an immutable transaction receipt. A
conflict does not partially update the canvas; the UI reconciles current state,
highlights failed preconditions, and lets the creator or Agent re-plan.

Reversible local Agent edits may commit without a modal approval. Their receipt,
affected range, and undo action remain immediately visible. High-impact actions
stay pending until the creator decides them.

One Workspace history surface lists recent durable creative transactions
newest-first across Creator and Agent actors. It is the sole owner of default
Undo/Redo targeting. Narrative, Transcript, rough-cut and Timeline panes publish
receipts to it and do not render independent "last change" Undo stacks.

CaptionDraft is a separate ephemeral inspector surface. Narrative offers an
explicit `Create captions` action for an exact SourceExcerpt; the draft lists
planner-compatible visible Clips and Caption Tracks, visibly ranks exact
Alignment/source-stream evidence, and blocks ambiguity. Preview rows show final
cue text and exact ranges but do not become editable fields. Apply refreshes
Sequence and the same global history; it does not create a pane-local Undo stack.

Selecting one committed Caption opens a manual Caption inspector independently
from Agent `@` context. A new manual Caption captures explicit In and Out marks
from the existing Sequence Viewer playhead and requires one Caption Track,
canonical language, and non-empty text; no default duration is hidden in the
gesture. Existing values remain exact until explicitly changed.

The inspector displays dependent Alignment handling as part of the gesture.
Timing-only edits may visibly request preserve-if-provable; text/language edits
and remove require an explicit stale or unbind choice. Same-Track overlap is a
blocking validation result rather than an overwrite mode. Local text survives
conflict, successful checkpoints enter the one Workspace history, and Caption
selection never auto-attaches itself to the Agent composer.

Direct source placement is a separate ephemeral inspector. It combines current
Source Viewer marks with explicit compatible Track/SourceStream bindings and a
captured Sequence Viewer destination playhead and pinned revision. A changed
Sequence revision makes the destination stale instead of following the moving
playhead. `Use full selected source` shows the exact selected-lane finite
coverage intersection; a lane with unknown duration blocks the action rather
than becoming a hidden default. One place action creates one unlinked Clip or
one linked A/V pair through the Creator planner. Success keeps the source
session, clears placement marks/draft, adopts and seeks the new paused Sequence
revision, switches to Sequence mode and refreshes global history. It never
creates a Narrative Alignment, Track, pane-local Undo stack or Agent attachment.

## Export delivery

Export is a revision-pinned delivery center rather than a fire-and-forget
button. The compact next-export strip names the exact Sequence revision, output
filename and registered preset before work starts. Recent lineage cards keep
durable job history visible and project one mutually exclusive action state:

- blocked, queued or running work exposes cancellation only;
- a ready artifact exposes Save As and an explicit two-gesture delete;
- deleted or recoverable terminal work exposes retry;
- a successful Save As exposes its receipt-bound Reveal action.

Delete confirmation exists only for a concrete ready artifact. A missing
artifact never compares equal to an empty UI selection, and running work never
renders permanent deletion controls. Deleting removes exported media while
retaining the durable job lineage.

## Progressive complexity

Default mode emphasizes intent, assets, transcript, viewer, and a compact rough
cut timeline. Precision controls appear when the creator selects an entity or
opens its inspector. The underlying model is never simplified into lossy state
that later has to be replaced by a professional mode.

Keyboard editing, exact time entry, track controls, and detailed properties may
grow without creating a second project format.

## Offline and degraded behavior

- Manual editing remains available when the local Agent is absent.
- Available artifacts render while other MediaJobs continue.
- Missing referenced originals retain clips, transcripts, and PaperEdit context.
- Restart reconstructs the workspace from Project snapshots and activity cursor.
- Waiting approvals and jobs remain visible without a live Agent process.
- A UI projection gap reconciles from current contracts state rather than
  replaying model prose or directly reading storage.

## UI invariants

- UI and Agent changes share the same transaction and revision semantics.
- No panel owns a private copy of creative truth.
- No conversation message is treated as a committed edit receipt.
- No UI component calls the Agent's internal tools on its behalf.
- No Agent command manipulates UI coordinates, DOM state, or hidden selection.
- No preview optimization changes the sequence or alignment model.
- No draft overlay is presented as committed creative truth.
- No internal transport or data path is surfaced as creator workflow.
