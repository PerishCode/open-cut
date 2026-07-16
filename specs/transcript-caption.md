# Transcript corrections and caption derivation

Status: Business design baseline.

## Recognition truth

A TranscriptArtifact is immutable operational output pinned to an Asset complete
fingerprint, source audio stream, engine/model identity, language policy, and
producer schema. Its segment/token IDs and exact source ranges remain stable
inside that artifact. Selecting a newer compatible artifact does not rewrite the
older artifact or any creative reference.

The initial recognition profile is the payload-qualified local transcription
engine plus the authenticated multilingual small model ProductResource. A
Transcript job remains blocked on both the exact executor and model
registrations. Creator resource acquisition may satisfy `model-required`; it
never edits a Project or lets an Agent choose supply-chain or storage inputs.
The transcription executor re-verifies the complete model digest immediately
before use and records the exact engine closure, catalog-entry identity, model
content digest, source fingerprint/stream, and language policy in the immutable
artifact producer identity.

The v1 input is the selected immutable audio SourceStream, not a proxy track.
FFmpeg deterministically maps that exact stream to mono, 16-kHz, signed 16-bit
PCM while preserving its first presentation timestamp and filling timestamp
gaps. The artifact records source start, exact sample count/byte count, PCM
digest, channel policy, and timing policy. Mono and stereo have explicit v1
mix policies; layouts outside the declared set fail with a typed diagnostic
instead of accepting an ambient downmix. A source with no audio succeeds with
`no-audio` and produces no fake TranscriptArtifact; valid speechless audio may
produce an immutable artifact with zero segments.

Recognition fixes automatic language detection, original-language output,
one thread/processor, CPU-only execution, disabled temperature fallback, word
timestamp output, and a strict bounded JSON result. Detected languages are
canonical BCP-47 tags, ranges are derived from the normalized PCM sample grid,
and token text is retained only when its ordered lexical evidence exactly
reconstructs the segment text. Confidence values are stored as basis points.

The Whisper JSON adapter recognizes only the closed uppercase/digit/underscore
`[_...]` control-token shape emitted by the qualified engine and omits those
zero-duration controls. A zero-duration orthographic token such as punctuation
is folded into the adjacent positive-duration lexical token so the token stream
still reconstructs the exact segment text. The merged token carries no
confidence because the product will not fabricate independent timing or
confidence for point evidence. Every input offset must remain inside the segment
and every retained positive-duration token remains ordered and source-ranged;
unknown controls, dangling point prefixes, overlap, or lexical mismatch reject
the complete recognition result.

`transcript read` is bounded and artifact-addressable. The first ready artifact
becomes the default. Re-transcription never silently changes that choice; the
Creator may compare artifacts and perform an expected-current compare-and-swap
selection. Agent reads may name an artifact but the Agent cannot mutate the
Creator's default selection.

## TranscriptCorrection

A creator correction is a first-class Project creative entity:

```text
TranscriptCorrection
  id / entityRevision
  assetId
  transcriptArtifactId
  segmentIds[]
  sourceRange
  replacementText
  language
  provenance
  tombstone
```

The source range is exact and the segment list proves the recognition evidence
that was corrected. Its start and end must coincide with token boundaries, its
segments must be contiguous, and every listed segment must contribute evidence
to the selected span. Replacement text is non-empty, UTF-8, and has no leading
or trailing whitespace. Corrections in the same language layer may not overlap.
Changing text or language advances the correction entity revision; changing the
pinned artifact or range requires an explicit remove/add replacement.

`transcript read` returns original recognition and effective corrected text as
separate typed fields with correction IDs/revisions. It never forges new token
timings for replacement words. Narrative source excerpts may cite original
segments plus corrections, preserving evidence and authorship.

Re-transcription creates another artifact. Existing corrections remain pinned
and visible; adopting/remapping them requires an explicit proposal with exact
old/new evidence. Failure to prove correspondence leaves them stale rather than
silently applying similar text.

Corrections are created, updated, and removed only through the ordinary
`edit propose` -> `edit apply` transaction path. There is no transcript-write
command, mutable artifact endpoint, or UI-only correction store.

## SourceExcerpt

A `source-excerpt` is one variant in the common ordered NarrativeNode tree.
Section, authored text, source excerpt, visual intent, and note share one stable
identity/parent/order/revision/tombstone projection; typed payload tables do not
create parallel ordering truth. An excerpt pins:

- Asset ID and accepted complete fingerprint;
- TranscriptArtifact and SourceStream IDs;
- a contiguous segment list and exact token-bounded source range;
- canonical language and the complete set of overlapping correction
  ID/revision pairs;
- immutable effective text resolved during proposal normalization.

An excerpt may cite a correction created earlier in the same proposal through a
proposal-local reference. Normalization requires the cited correction set to be
exactly the active same-language corrections overlapping the selected range;
omitting, adding, or citing the wrong revision is invalid. Replacement words
remain semantic text only and never gain fabricated token timings.

Narrative reads derive `exact | stale` against the current Asset fingerprint,
artifact readiness/source identity, and correction revisions. A later relink,
artifact eviction, correction update, or correction tombstone does not rewrite
or delete the excerpt: its accepted evidence and effective-text snapshot remain
visible as `stale` until an explicit edit replaces it. Transcript reads likewise
return immutable source segments and authored correction projections in separate
fields.

The evidence payload has no update operation. Reordering uses the generic
NarrativeNode move; removal is a tombstone through the generic remove operation.

Creator SourceExcerpt insertion uses an explicit contiguous token selection from
one exact TranscriptArtifact projection. The first click anchors one token; the
second extends to another loaded token, and the selected interval includes every
token between them in recognition order. Selected segments must be contiguous,
the range starts at the first token start and ends at the last token end using
exact rational arithmetic, and the operation carries at most 256 segment IDs.
Browser DOM text selection, copied display text, locale word boundaries, and
rounded seconds are never evidence inputs.

Every current TranscriptCorrection in the selected language that is completely
contained by the token interval is included with its exact revision and entity
precondition. A correction that only partially overlaps either boundary blocks
insertion and requires the Creator to widen or change the selection. The API
recomputes effective text from immutable recognition tokens plus those exact
corrections; the Web never submits copied or locally rewritten excerpt text.

The Creator target is an explicit controller-owned Narrative insertion anchor:
exact parent identity/revision plus an optional loaded sibling anchor. Selecting
a Narrative node sets the insertion point after that node in its parent;
otherwise the root loaded-page tail is the default. A successful insertion
advances the anchor to the allocated SourceExcerpt. Stale parent or correction
revisions conflict rather than silently moving or rebasing the excerpt.
Changing the selected evidence creates a new SourceExcerpt identity.

## Caption derivation

Caption items are independent Sequence creative entities. They are created only
by explicit operations or a persisted caption-derivation proposal that pins:

- transcript artifact and correction revisions;
- exact selected source/timeline ranges and Clip mappings;
- language;
- a versioned segmentation policy with expanded maximum lines, characters,
  duration, gap, punctuation, and reading-rate settings;
- resulting caption IDs, canonical BCP-47 languages, text, exact timeline
  ranges, and provenance.

Derivation is deterministic proposal normalization, not an operational job that
edits the Sequence after completion. The creator or Agent reviews/applies the
normal EditProposal. Once committed, captions evolve independently: later
recognition, correction, Narrative, or segmentation-policy changes never rewrite
them.

The initial policy is `readable-captions-v1`; all its defaults are expanded into
the proposal digest. Its first immutable definition is:

```text
maximum lines                    2
maximum Unicode EGCs per line   42
minimum duration                1 s
maximum duration                6 s
maximum inter-unit gap          750 ms
maximum reading rate            20 EGC/s
boundary policy                 terminal-punctuation-v1
timing policy                   forward-pad-no-overlap-v1
```

The Unicode segmentation implementation identity is pinned with the expanded
policy. Line capacity counts extended grapheme clusters rather than bytes,
codepoints, or display cells. Terminal punctuation is a preferred boundary once
the current cue satisfies its minimum duration; capacity, gap, and maximum
duration remain hard boundaries.

A TranscriptCorrection replacement is one indivisible semantic unit. It may not
be split to manufacture timings for words the recognizer never timed. If a
single replacement cannot fit the fixed policy, derivation fails with a typed
validation issue and the creator must explicitly revise the correction or use
manual captions.

Each derived cue begins at its first evidence time. The policy may extend only
the cue end, never its start, to meet minimum duration or reading rate. Padding
may not cross the next cue, the selected SourceExcerpt, or the named Clip. An
unsatisfiable constraint fails the whole derivation; the product never overlaps,
retimes, drops, or silently splits cues to force a result.

`edit derive-captions` is a bounded, read-only deterministic preview. It accepts
one exact existing SourceExcerpt, one exact existing Clip, one caption Track,
and a proposal-local output prefix. It returns the base revision, complete
preconditions, the expanded policy, and ordered explicit output tuples:

```text
caption local symbol / alignment local symbol
source range / timeline range / final text
```

The preview does not reserve IDs, persist a proposal, or change creative state.
The Agent submits that exact operation through the sole structured write entry,
`edit propose --input -`. Proposal normalization re-derives the cues and rejects
missing, reordered, altered, or injected outputs. It may suggest boundaries from
recognition tokens, but the normalized proposal stores every final Caption and
Alignment explicitly; `edit apply` verifies and commits those immutable bytes
without running segmentation again.

The first request is single-Clip by construction. Reusing one SourceExcerpt in
multiple Clip instances requires one explicit derivation operation per Clip;
independent operations may coexist in one bounded proposal. More than 128 cues
is invalid and is never auto-split because doing so would change transaction,
review, and undo boundaries.

### Creator caption review

The first-party Creator uses the same deterministic `readable-captions-v1`
planner through a Creator-only semantic preview edge. The Agent command remains
the sole Agent-visible entry. Creator preview pins one SourceExcerpt identity and
revision, one Clip identity and revision, and one Caption Track identity and
revision. Contracts owns the exact preconditions and derivation operation; Web
receives only semantic cue rows and cannot construct generic edit bytes.

The CaptionDraft controller owns these three selections independently from
Timeline primary selection, Narrative insertion anchor, and Agent `@` context.
An exact Narrative-to-Clip Alignment may rank compatible Clip candidates but
does not replace the explicit Clip input and is not required. Candidate proof
requires the same Asset and complete source-range coverage. A matching
transcript SourceStream is a visible recommendation when several linked lanes
are possible, never an implicit choice. A unique Caption Track may be visibly
prefilled; ambiguous Tracks remain blocking.

Creator derivation is initially `insert-only`. Any overlap on the selected
Caption Track rejects the whole preview; there is no implicit replace, merge,
ripple, shift, or deletion. One preview covers one SourceExcerpt-to-Clip mapping
and one to 128 cues. Multiple SourceExcerpts are separate explicit gestures.

Review shows the final language, expanded policy, ordered text, exact source and
timeline ranges, and Caption/Alignment counts. Cue content is immutable inside
the review: the Creator either changes TranscriptCorrection evidence and
previews again, or applies and later performs an explicit manual Caption edit,
which correctly makes provenance content `modified`. Apply is one direct
Creator transaction. A known conflict requires a new preview; an ambiguous
apply may only replay the identical opaque review and request identity.

Re-derivation never repairs committed captions in the background. Existing
overlap remains an explicit blocking condition. A future atomic replacement
operation must name the complete old Caption identity/revision set; it cannot
infer ownership from a time range or provenance label.

### Manual Caption evolution

Creator manual Caption create/update/remove uses the ordinary Caption operation,
normalization, provenance, transaction, inverse, and activity kernel. A
Creator-only semantic planner closes exact Track, collision, and dependent
Caption-Alignment state; Contracts keeps its exact operation envelope opaque.
The Agent continues to reach the same kernel only through CLI proposal/apply.

Manual creation has `manual` provenance and no implicit transcript or Narrative
relationship. Updating a transcript-derived Caption never discards or rewrites
its derivation provenance. Reads compute `content: modified` whenever the final
range, language, or text differs from the recorded derivation result.

Exact Caption Alignment preservation is deliberately narrower than ordinary
revision remapping. It is allowed only for a timing-only update whose Caption
target identities and local ranges are unchanged and still fit the final
duration. Text or language replacement must explicitly stale or unbind every
exact dependent Alignment. Removal must stale or unbind and cannot preserve.
Same-Track overlap remains a whole-gesture failure with no implicit repair.

## Alignment and timing

Caption provenance may reference TranscriptArtifact segments,
TranscriptCorrections, NarrativeNodes, Clips, and the deriving transaction.
Narrative Alignment targets Caption local ranges under the ordinary revision
rules. Updating caption language, text, or range remaps or stales dependent
Alignments explicitly.

Every Caption has typed provenance. A manually created Caption records `manual`.
A transcript-derived Caption records `transcript-derivation` and pins the exact
SourceExcerpt revision, Asset fingerprint, TranscriptArtifact, SourceStream,
contributing segment IDs and correction revisions, Clip revision plus its source
and timeline mapping snapshot, evidence source range, expanded policy, and the
derived language, text, and range. Migration assigns `manual` to captions that
predate this provenance schema; it does not guess historical derivation.

Reads project transcript-derived provenance on two independent axes:

- content is `exact` while the committed Caption language, text, and range still
  equal the derivation result, otherwise `modified`;
- evidence is `exact` while the cited SourceExcerpt and Clip evidence still
  matches, otherwise `stale`.

Both axes are explanatory and never rewrite or disable the Caption. A creator
may deliberately keep modified content over stale evidence. Re-derivation is a
new explicit proposal, not a background repair.

Source transcript time becomes timeline time only through named Clip source-to-
timeline mappings. A source range used in multiple clips can create multiple
caption items; there is no hidden global transcript-to-sequence synchronization.

## Harness

- preserve immutable recognition bytes while creating/updating/tombstoning
  corrections;
- reject overlapping corrections and artifact/segment/range mismatch;
- re-transcribe and prove no existing correction, excerpt, or caption changes;
- snapshot `readable-captions-v1` normalized outputs and proposal digest;
- apply a caption proposal after producer completion and prove segmentation is
  not rerun at apply time;
- map one source excerpt into multiple Clip instances and require explicit final
  Caption ranges for each.
