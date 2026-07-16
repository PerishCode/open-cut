# Viewer playback clock and revision pinning

Status: Business design baseline.

## Playback session

Playback is an operational Viewer session over one immutable source or Sequence
projection. It never advances creative revisions or changes selection by hidden
side effect.

A Sequence playback session pins:

```text
projectId / observedProjectRevision
sequenceId / sequenceRevision
RenderPlan digest
SequencePreviewArtifact and MediaLease identities
semantic duration / frame rate+count / sample rate+count / tail policy
exact start playhead
clock mode
```

A Source playback session separately pins one Asset revision and accepted
fingerprint, one explicit video and/or audio SourceStream selection, the source
proxy artifact/lease, its exact source epoch and track starts, finite selected
coverage, and one exact absolute-source playhead. Source and Sequence sessions
never share or overwrite playhead state.

The observed Project revision is audit/presentation metadata and does not select
render semantics. Sequence revision and RenderPlan digest are authoritative.

An incoming newer Sequence revision does not hot-swap the plan during playback.
The Viewer shows that a newer revision is available and adopts it after pause
and plan reconciliation. Beginning a creative edit gesture first pauses the
session, fixes the gesture base revisions, commits, and then compiles a new plan.

Activity reconciliation updates only `availableRevision`. Adopting it is one
explicit controller command: pause, freeze the exact logical playhead, abort the
old preparation generation, prepare the chosen revision without the old
continuation, clamp the playhead against the new semantic duration, and remain
paused. The post-commit Viewer path invokes this same command; no React effect,
poll response, or lease expiry may imply adoption.

Operational proxy readiness may replace a degraded input only when the artifact
declares compatibility with the same pinned plan input and exact time map. It
cannot switch creative revision.

## Master clock

With audible audio, the audio device presentation clock is master. Video frames
are scheduled or dropped against that clock; video timing never drives audio
resampling implicitly. With no audible audio route, a monotonic high-resolution
clock is master.

Wall-clock time, `Date.now`, media-element rounded seconds, and animation-frame
counts are not authoritative. The playback adapter maps clock duration to exact
RationalTime relative to the pinned start and reports an exact playhead.

At each presentation decision, Sequence video is sampled at the exact frame
boundary implied by its rational frame rate. Audio retains sample/sub-frame
precision. Display formatting may round but never feeds back into seek or edits.

The immutable Sequence preview has a zero timeline epoch and fixed CFR/sample
grids. The adapter therefore needs no per-frame output map: it derives exact
frame and sample positions from the pinned counts and rates, clamps only at the
declared presentation end, and preserves `semanticDuration` separately from
black/silent tail padding. Browser `currentTime` is an actuator/observation,
never the stored logical playhead.

## Seek, scrub, and step

- seek names an exact RationalTime and resolves the pinned RenderPlan at that
  position;
- scrubbing is creator-controlled and may use lower-cost keyframe/proxy previews,
  while the settled playhead remains exact;
- frame-step advances or retreats by one Sequence frame rational, independent
  of source frame rate;
- source mode seeks in exact source time and maps through the selected stream's
  timestamp/proxy map;
- out-of-range seeks clamp only through an explicit Viewer policy and report the
  normalized settled position.

Variable-frame-rate source decoding selects the frame whose declared timestamp
policy covers the requested source time. Container frame indexes and browser
`currentTime` are never durable edit coordinates.

Browser media time is offset through the proxy's exact source epoch and then
settled onto the selected source time-base/sample policy before it may become a
Source Viewer mark. VFR frame-step resolves a bounded predecessor/successor
through the verified proxy time map behind the Contracts Viewer port; React
never downloads a raw whole-file map or promotes a float observation to
creative truth.

With video selected, its presentation boundaries are the Source Viewer editing
grid even when audio is also selected. An audio-only selection uses exact source
sample boundaries. `Mark In` captures the current settled boundary. `Mark Out`
captures its next exclusive boundary, or a proven finite coverage end when no
later presentation/sample boundary exists. It never reuses the current boundary
as a zero-duration Out or guesses beyond unknown coverage. `Use full selected
source` computes the positive intersection of all selected finite coverages;
if any selected lane lacks a finite duration, the action blocks and requires
manual In/Out.

## State and degradation

The operational state union is at least:

```text
idle | preparing | paused | playing | seeking | buffering | degraded | ended | failed
```

MediaLease expiry, missing proxies, and source loss produce typed state and
activity/job references. Buffering does not move the exact logical playhead
without clock evidence. Resuming after renewal verifies the same pinned revision
and plan digest.

One headless Sequence Viewer controller owns the operational state, request
generation, continuation, polling, renewal, pinned revision/plan, and exact
playhead. Contracts remains the validated product transport port. React only
subscribes to controller snapshots, and the media component is an actuator for
the controller; neither owns timers or a second playback truth. Activity may
wake a poll early, but bounded continuation polling and expiry-derived renewal
remain sufficient for progress.

One parallel headless Source Viewer controller owns explicit stream selection,
lease generation, renewal, exact absolute-source playhead and local In/Out
marks. The media element remains an actuator. Changing Asset revision,
fingerprint or selected streams invalidates marks instead of silently mapping
them onto another source projection.

## Harness

- run audio-master and monotonic-master playback against a deterministic clock;
- verify frame-step and long-duration rational playhead without float drift;
- inject dropped video frames and prove audio time and edit coordinates remain
  exact;
- commit from another window during playback and prove no hot plan switch;
- begin a creative gesture and prove pause -> commit -> new-plan ordering;
- seek variable-frame-rate fixtures through proxy maps and compare selected
  source timestamps with the reference decoder.
