# Interactive preview and the draft overlay contract

Status: Business design baseline.

## Purpose and position

`specs/editing-interaction.md` gives creator gestures an ephemeral `EditDraft`:
pointer movement updates a local overlay and preview input, and pointer-up
commits exactly one transaction. `specs/rendering.md` makes the committed
preview a pure function of one pinned Sequence revision. Today those two
baselines meet only at accessory presentation: a draft can move ghost
rectangles, but composed frames always come from the last adopted revision,
prepared whole.

This baseline names the missing contract between them. It exists so that every
future creative capability — transforms, keyframes, audio curves, transitions,
speed — inherits one shape for interactive feedback instead of inventing its
own, and so the revision engine never has to absorb gesture-frequency writes to
feel fast.

## Two tiers, one vocabulary

The commit tier is unchanged. Revisions advance at gesture and intent
boundaries only; the journal, projections, activity, undo, preconditions, and
every durability or concurrency guarantee live here and only here.

The draft tier is the `EditDraft` of `specs/editing-interaction.md`: one
gesture, one draft; renderer-local, single-writer, discardable, never
observable outside the renderer, never durable.

The load-bearing invariant is shared vocabulary. A draft holds normalized
candidate operations from the same operation union the commit tier journals.
Promoting a draft is submitting those operation payloads unchanged; there is no
translation layer, no draft-only operation dialect, and no capability that
exists in one tier but has no meaning in the other.

## Preview input contract

Interactive preview consumes:

```text
PreviewInput
  plan          pinned RenderPlan (one committed Sequence revision)
  overlay       DraftOverlay | empty
DraftOverlay
  baseRevisions the draft's base entity revisions
  operations    the draft's normalized candidate operations (bounded)
```

The engine's obligations:

- Frames are a pure function of (plan digest, canonical overlay form). An empty
  overlay reproduces the committed preview byte-for-byte; overlay application
  never mutates the plan or any committed material.
- Overlay operations compile to instruction deltas against the plan's existing
  instruction vocabulary. The overlay is not a second effect graph; if a
  capability cannot be expressed as plan instructions, it is not ready for the
  draft tier either.
- An overlay whose base revisions no longer match the pinned plan is stale as a
  whole and is never partially applied.

## Incremental invalidation

The dirty set of an overlay (or of a newly adopted revision) is the union of
the timeline ranges and composition layers its instruction deltas touch. The
engine re-renders only dirty range-by-layer regions; everything else is served
from the pinned plan's prepared materials.

Gesture updates coalesce latest-wins. A frame render made stale by a newer
overlay is abandoned, never presented late. When the dirty work of the current
overlay exceeds the interactive budget, the engine presents the last clean
frames plus the accessory overlay presentation (today's ghost language) instead
of blocking the gesture or pretending smoothness it cannot deliver.

## Commit, cancel, and conflict

Pointer-up commits the draft's operations as one transaction, unchanged from
`specs/editing-interaction.md`. The viewer then adopts the new revision. When
the committed operations are byte-identical to the final overlay, frames the
engine already rendered for that overlay may seed the new revision's prepared
materials under a digest check; otherwise preparation proceeds normally.

Cancel drops the overlay with zero residue. A commit refused by preconditions
keeps the draft for inspection and presents the established refusal language —
typed blocked reasons, conflict recovery, and pending-work-loss notices. A
draft is never silently rebased, and a refusal is never presented as an empty
timeline that merely snapped back.

## Capability precedence

At the commit tier the stable CLI is never a narrower surface than the first
party UI: any committed truth the UI can produce must be producible through the
registry-derived CLI, byte-identically. The schema registry is the single
attachment point that makes this structural — an operation family exists for
every surface the moment it registers, and the family order below puts CLI
reachability at step one, before any gesture work.

Draft-tier affordances — overlays, ghosts, refusal and loss presentation — are
interaction feedback, not capability; they carry nothing committable that the
CLI cannot submit directly, so their UI exclusivity does not breach precedence.

Deliberate authority edges may restrict the Agent below the creator — today:
project genesis and media import, both consent-bound creator transactions, and
the typed gesture-preview outcome, which remains a creator-port convenience
until its vocabulary reaches the CLI. Every such edge must be enumerated here
or in the owning baseline; an unenumerated capability gap between the surfaces
is a defect, not a policy.

## The feature family shape

Every future creative capability is added in this order, and the first two
steps gate the rest:

1. an operation family in the schema registry, with normalization, validation
   reasons, and stored inverses;
2. the plan instruction vocabulary it compiles to;
3. its overlay delta mapping under this contract;
4. its gesture binding and refusal presentation under
   `specs/editing-interaction.md`;
5. nothing: the CLI and Agent surface arrive from the registry unchanged.

The first family implemented under this contract is static visual transforms —
position, scale, rotation, opacity, crop as clip properties — followed by
keyframes over those same properties. Speed and time remapping are explicitly
deferred: they break the linear source-to-timeline mapping that alignments and
caption derivation currently assume, and they require their own baseline before
an operation family exists.

## Acceptance for the transform pilot

- Dragging a transform control updates composed frames within the interactive
  budget while the journal records nothing.
- Pointer-up produces exactly one transaction; undo restores the prior
  committed frames exactly.
- Cancel leaves zero residue at both product sizes.
- A concurrent commit landing mid-gesture surfaces the established conflict and
  loss language; the draft never silently reverts.
- With an empty overlay, equal plan digests produce byte-stable frames — the
  committed preview's purity is untouched.

## Non-goals

No second mutation model, no durable drafts, no gesture-frequency revisions,
no Agent access to the draft tier (the Agent remains commit-tier through the
stable CLI), and no general effect graph reachable from either tier.
