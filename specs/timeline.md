# Timeline and edit semantics

Status: Business design baseline.

## Scope

The timeline is the deterministic executable model used by viewer preview and
export. Narrative describes intent; only committed sequence state determines
what is rendered.

The first slice is a semantic rough-cut editor, not a general effect or
compositing engine.

## Exact time

All source and timeline time uses a reduced rational representation:

```text
RationalTime
  value: signed int64, encoded on JSON wires as canonical decimal string
  scale: positive int32
```

`value / scale` represents seconds exactly. A `TimeRange` is `start` plus
positive duration. Implementations normalize values for comparison and
checked arithmetic; overflow and incompatible invalid scales reject a
transaction.

Frame rate is rational. Frame snapping is a command policy, not a loss of exact
source time. Audio ranges may retain sub-frame precision. Floats are display-only.

Source time is the original presentation timestamp coordinate of the accepted
SourceStream, not a zero-based duration offset and not proxy `currentTime`.
When a descriptor has `startTime` and `duration`, its finite editable coverage
is exactly `[startTime, startTime + duration)`. An absent start has the explicit
zero epoch; an absent duration provides no finite `full source` range and cannot
be guessed by Creator placement. Source proxy rebasing is operational and does
not change committed Clip ranges.

Non-time geometry uses the separately typed, identically canonical
`ExactRational`. Crop and anchor coordinates are bounded basis points; scale is
a dimensionless exact ratio; translation is an exact signed fraction of the
Sequence canvas width or height. `RationalTime` is never reused for these
values. Render evaluation applies orientation, crop, fit, anchor-relative
scale, then canvas-relative translation in that order.
The normative JSON and TypeScript rules are defined in
`specs/wire-contract.md`; exact fields never cross a product wire as unsafe JSON
numbers.

## Sequence

A Sequence contains:

- opaque ID, entity revision, name, and creator-visible role;
- canvas dimensions, pixel aspect, rational frame rate, and audio format;
- ordered video, audio, and caption tracks;
- a derived duration from committed contents.

Projects support multiple sequences, but the first slice creates one `main`
sequence. Export pins its sequence ID and entity revision.

## Track

Tracks have opaque IDs, stable ordering keys, and one type:

- `video`;
- `audio`;
- `caption`.

Track order changes through an explicit operation. Array index is never a command
precondition or durable identity.

Clips on the same video or audio track may not overlap in the first slice.
Caption items on the same caption track also may not overlap; multiple explicit
caption tracks can represent independent languages or layers. Transitions and
overlap-based compositing are deferred.

## Clip

A source clip contains:

```text
id
trackId
assetId
sourceStreamId
sourceRange
timelineRange
enabled
linkGroupId?
gain?
reframe?
```

Source and timeline durations must agree unless an explicit rate operation is
introduced in a later schema. The first slice has no implicit speed change.

A clip references an Asset and one explicit product source-stream ID. Missing
source media degrades preview and export but does not invalidate clip identity
or silently remove it.

Typical camera media is assembled as separate video and audio Clip entities on
compatible tracks with a shared `linkGroupId`. The normalized operation names
the exact video and audio stream IDs; a detected primary stream is never hidden
render behavior. Video-only, audio-only, and alternate-stream edits remain valid.

`LinkGroup` is a durable revisioned Sequence entity, not an incidental shared
string or a property inferred from matching ranges. Membership is expressed only
by each Clip's optional `linkGroupId`. Group revision changes make membership
changes conflict-detectable, and a tombstoned group cannot accept new members.

The first implemented `add-clip` input names `createAs`, `trackId`, `assetId`,
`sourceStreamId`, exact `sourceRange`, exact `timelineRange`, and `enabled`. It
either omits grouping, creates a group through proposal-local
`createLinkGroupAs`, or references one through `linkGroup`. A typical A/V insert
is two ordered `add-clip` inputs: the first creates the group and the second
references that proposal-local identity. Normalization emits the group before
its Clips; the stored inverse removes Clips before the group, so apply and undo
remain atomic and foreign-key safe.

## Reframe

Sequence format defines canvas aspect. A clip may carry deterministic reframe
state: normalized crop, scale, translation, and optional fit policy. Automatic
subject tracking may propose values but never becomes hidden render behavior.

All reframe values committed by an agent are inspectable, reversible sequence
state.

## Captions

Caption items are authored sequence entities with exact timeline ranges, text,
and an explicit canonical BCP-47 language. `und` is a committed value, not a
missing-field default. Runtime locale, UI locale, and transcription guesses
never change an already committed Caption language. They may reference
transcript segments or authored NarrativeNodes for provenance, but later
transcript changes do not silently rewrite captions.

Transcript corrections and versioned caption segmentation create only explicit
proposal operations under `specs/transcript-caption.md`. Apply never reruns a
caption generator, and captions evolve independently after commit.

Every Caption records typed `manual | transcript-derivation` provenance. Reads
project derived captions on independent content `exact | modified` and evidence
`exact | stale` axes. These states explain origin and drift; they never disable,
rewrite, or background-repair committed timeline content.

Caption styling begins as sequence-level defaults plus bounded item overrides.
A general motion-graphics document is not part of the first slice.

## Initial operations

The closed operation set includes:

- `create-sequence`;
- `add-track` and `move-track`;
- `add-clip`;
- `split-clip` at an exact timeline time;
- `trim-clip` with explicit source and timeline ranges;
- `move-clip` to an explicit track and start;
- `remove-clip` as creative tombstone;
- `set-sequence-format`;
- `set-clip-reframe`;
- `add-caption`, `update-caption`, and `remove-caption`, with add/update carrying
  the complete language together with range and text;
- `link-clips` and `unlink-clips`.

Operations never infer ripple behavior. Any operation that shifts neighboring
items must include the complete intended shift set in the same EditTransaction.
This makes paper-edit assembly deterministic and diffable.

Split, trim, move, and remove operations on a linked Clip require an explicit
`scope: linked | single`; CLI and UI help recommend `linked`, but no value is
defaulted outside the request or omitted from normalized proposal content.
Linked scope pins the LinkGroup revision and every live member revision, then
expands one atomic stream-correct operation while preserving current relative
sync. Single scope changes timing/content only for the named Clip. If a single
operation would leave fewer than two live members, normalization explicitly
clears membership from every survivor and tombstones the now-meaningless group;
singleton and empty live LinkGroups are invalid final state.

Move input names the seed Clip's final destination Track and timeline start;
linked members keep compatible Tracks and receive the same exact timeline delta.
Trim input names the seed Clip's complete final source and timeline ranges;
linked members receive the same exact head/tail deltas. These are final-state
operations, not UI pointer deltas. A Clip may be the subject of at most one
mutation or linked expansion in a proposal; callers collapse repeated edits to
one final state.

Split input names an exact timeline point and an explicit bounded output mapping
from every Clip in scope to proposal-local left/right Clip identities. Linked
split also names proposal-local left/right LinkGroups, places every left output
in the left group and every right output in the right group, and tombstones the
old group. Single split outputs are unlinked. The original Clip identities are
always tombstoned; split never changes what an existing identity means.

Removing a Clip tombstones it. Linked removal tombstones every live member and
the group; single removal applies the group-dissolution rule above. Linking and
unlinking are explicit reversible operations. `link-clips` names two to 64 live,
currently unlinked Clips in one Sequence and creates one proposal-local group.
`unlink-clips` names one live group and dissolves it completely; partial hidden
subgroups are forbidden.

## Validation

Before commit, the timeline validates:

- referenced sequence, track, clip, asset, and alignment IDs;
- entity revision preconditions;
- positive source duration and non-negative timeline start;
- source ranges within known asset duration when the source is online;
- track type compatibility;
- source-stream existence and media-type compatibility;
- linked-operation membership, generation, and relative sync;
- first-slice non-overlap constraints;
- exact arithmetic and output format validity;
- alignment effects declared by the transaction.

Validation produces indexed typed issues. It never partially applies valid
operations from an invalid transaction.

## Split, trim, and move semantics

Splitting creates new clip identities for every item in declared scope and
tombstones the originals. A linked A/V split uses the same exact timeline point,
stream-appropriate source times, and two new groups as described above. The
transaction records the explicit output identity mapping and remaps affected
clip-range Alignment targets to the new local ranges or marks them stale.

Trimming preserves clip identity when one contiguous source range remains. Moving
preserves clip identity and source range. A move may preserve exact Alignment
anchors by updating the Clip revision and derived absolute range in the same
transaction. Removing a clip tombstones it and marks dependent exact alignments
stale or unbound as declared by the transaction.

Every mutation of a Clip referenced by an exact Alignment must be paired in the
same proposal with exactly one `remap-alignment`, `mark-alignment-stale`, or
`unbind-alignment` operation. `remap-alignment` supplies the complete final target
set and may keep `exact` only when normalization proves source semantic coverage:
move preserves the same mapping, trim retains the whole anchored source range,
and split targets form contiguous, non-overlapping coverage of each old target
on the same Asset and SourceStream. A status label alone cannot claim exactness.

No command rewrites unrelated clips solely to close a gap. A higher-level agent
plan may include explicit moves for a ripple edit.

## Creator timeline gesture planning

The Creator Timeline never constructs a linked mutation from the currently
visible `SequenceWindow`. A bounded window cannot prove complete LinkGroup
membership or enumerate every exact Alignment that references an affected Clip.
Instead, pointer-up or an explicit inspector commit sends one semantic gesture
to a first-party Creator-only read planner. Its input pins:

- the seed Clip identity and revision;
- an explicit `single | linked` scope;
- one complete final move/trim/split/remove value;
- the destination Track revision when moving;
- `preserve-if-provable | mark-stale | unbind` Alignment handling;
- a proposal-local prefix for split outputs.

The planner reads the complete LinkGroup and exact Alignment closure, expands
all member revisions, collision dependencies, output identities and Alignment
operations, and returns a semantic affected-entity review plus an opaque exact
Creator edit envelope. It reserves no durable identity and performs no creative
or operational write. Contracts owns the envelope; Web cannot construct,
inspect or modify generic edit wire operations.

The planner result is a closed `ready | blocked` outcome. `ready` projects the
normalized Proposal into one semantic effect per affected original Clip (at
most 64), including before/final placement, split left/right roles and explicit
Alignment effects. Proposal-local identities and the generic wire operation
remain hidden. `blocked` contains a typed collision, preserve, compatibility or
range reason and only the explicit recoveries valid for that reason; it never
contains an apply envelope. The normalized Proposal is the sole authority for
`ready` effects rather than a second repository or UI implementation of Clip
mutation rules.

`preserve-if-provable` fails the whole plan unless every affected exact
Alignment can be remapped with identical source semantic coverage. Move keeps
the same Clip-local ranges, trim translates only fully retained local ranges,
and split partitions each old target into contiguous left/right local targets.
Remove cannot preserve. `mark-stale` and `unbind` name the complete affected
Alignment set; neither is inferred from a missing operation.

One Creator gesture may plan and immediately apply without a second review
click, but Apply still submits only the exact opaque envelope through the
existing one-call Creator commit kernel. A known conflict discards the plan and
requires a new gesture against current projection. Ambiguous delivery retains
the identical envelope, request identity and body for explicit retry. Planning
never creates an Agent receipt, Run or Turn, and does not add an Agent entry
outside the product CLI.

For direct manipulation, a scope containing any video Clip settles on the
Sequence frame grid; a pure-audio scope settles on the Sequence 48 kHz sample
grid. Pointer pixels and floating-point viewport coordinates are advisory only.
The headless Timeline controller produces the exact settled RationalTime and the
server still validates the complete storage closure. A linked move changes only
the seed Clip's compatible destination Track; every companion remains on its
current Track and receives the same exact timeline delta. Cross-lane Track-set
mapping is a separate future operation, never an inferred extension of move.

## Creator direct source placement

Direct placement creates ordinary `add-clip` operations without first creating
a SourceExcerpt. One gesture pins one accepted Asset revision/fingerprint, one
positive exact source range, one exact Sequence Viewer destination playhead and
pinned Sequence revision, and one or two explicit lane bindings. Each binding names one compatible
SourceStream and existing Track identity/revision; unique candidates may be
visibly prefilled, while ambiguity is blocking.

The range must be inside every chosen SourceStream's absolute presentation-time
coverage. `Use full selected source` is an explicit gesture that computes and
shows the exact intersection of the selected lanes; there is no hidden default
duration. Source and Timeline durations remain equal under fixed `1:1` rate,
new Clips are explicitly enabled, and any target-Track overlap rejects the
entire placement with no append, ripple, overwrite, trim or neighbor move.

One video plus one audio binding creates two Clips and one proposal-local
durable LinkGroup atomically. A one-lane placement creates one unlinked Clip.
Track creation is a separate operation and is never inferred from a missing
compatible lane. Direct placement creates no Narrative Alignment and needs no
parallel provenance aggregate: Asset, SourceStream and exact source range are
the Clip's committed source identity.

A first-party Creator planner reads current Asset/Stream/Track/Sequence and
collision state, returns semantic lane/range/group effects, and gives Contracts
an opaque exact add-clip envelope. Web cannot construct generic operations.
The Agent receives no planner API and continues to reach `add-clip` only through
the stable product CLI `edit propose` path. Known conflict requires a fresh
plan; ambiguous apply retains only the identical envelope and request identity.

## Creator Caption gesture planning

Creator manual Caption create/update/remove uses a separate semantic gesture
planner over the same edit kernel. Input pins the exact Caption Track revision;
update/remove also pin the Caption revision and an explicit Alignment handling
policy. Create carries one proposal-local Caption identity and the complete
range/language/text. Update carries the complete final range/language/text.

The planner reads all same-Track collision candidates and the complete exact
Caption-Alignment dependency set. Preserve remaps only identical Caption target
identities/local ranges after proving that text and language are unchanged and
the final duration retains every target. Stale/unbind names the complete
dependency set. Remove rejects preserve. The response exposes only semantic
subject and Alignment effects to Web; Contracts privately owns the exact
preconditions and operation list used by one atomic Creator apply.

## Preview

The viewer renders a project-revisioned sequence projection. Preview may use
proxies and lower-quality transforms, but it must preserve timing, ordering,
enabled state, crop, captions, and audio decisions.

Preview and export compile the shared normalized RenderPlan defined in
`specs/rendering.md`; they do not maintain separate interpretations of Sequence
state.

A preview optimization cannot write creative state. If source or proxy data is
unavailable, the viewer reports a typed degraded item rather than substituting
unrelated content.

## Export

An ExportJob pins:

- project ID and project revision;
- sequence ID and sequence entity revision;
- every referenced input Asset identity and accepted complete SHA-256;
- every selected proxy or other derived render-input artifact version used by
  the renderer;
- normalized export preset;
- a project-owned canonical ExportArtifact destination.

Later edits do not alter the running export. A missing or changed source fails
with typed asset diagnostics. Export never updates the sequence to match encoder
limitations.

The Agent receives export identity, state, media facts, and verification result,
not a canonical output path. Creator UI owns reveal and Save As flows; arbitrary
destination selection and overwrite authorization are separate from rendering.

## Narrative relationship

Narrative and sequence are peer creative models. One transaction may create
clips from source-excerpt nodes and bind exact alignments. Later manual timeline
editing either updates an alignment with proof or marks it stale.

The first deterministic materializer is defined in
`specs/paper-edit-rough-cut.md`. It consumes an explicit ordered SourceExcerpt
selection and explicit lane bindings, packs exact 1:1 Clip ranges without hidden
ripple or overwrite, and creates one multi-target Alignment for each excerpt's
complete video/audio realization.

No timeline operation rewrites narrative text as an implicit side effect.

## Deferred surface

The first slice defers transitions, rate changes, nested sequences, compound
clips, arbitrary effects, keyframe curves, multicam, advanced mixing, deep color,
and collaborative operational transform. Each requires explicit schema and
transaction semantics before implementation.
