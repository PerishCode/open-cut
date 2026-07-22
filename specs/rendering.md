# Preview and export rendering

Status: Business design baseline.

## RenderPlan boundary

Preview and export consume the same normalized v4 `RenderPlan` compiled from one
committed Sequence revision. The plan is an internal product artifact, not an
Agent editing surface or general effect graph.

A plan pins:

```text
projectId
sequenceId / sequenceRevision
renderPlanSchema / compilerVersion
sequence format and exact semantic duration
ordered visual, audio, and caption instructions
input Asset complete SHA-256 identities
selected compatible artifact identities
font and other deterministic resource identities
normalized output policy
digest
```

The Project revision observed while loading the consistent snapshot is retained
as audit metadata beside the plan, not inside its semantic payload. Unrelated
Project edits therefore cannot change a plan digest. Sequence, Track, Clip,
Caption, Asset, selected artifact, and resource revisions or identities remain
semantic pins.

The compiler validates all inputs before publishing the plan. Equal normalized
inputs under the same compiler version produce the same plan digest.
Defaults are fully expanded and the plan uses the domain-separated JCS/SHA-256
contract in `specs/canonicalization.md`; machine or encoder defaults cannot
remain behavior outside the digest.

A partial plan is never published. When an exact render material or product font is not
ready, preparation persists the selected producer job as a prerequisite and
waits. Compilation happens only after every selected immutable input is ready.

Every input is a profile-pinned render material. Preview currently binds
`webm-vp9-opus-source-v1`; export binds
`matroska-ffv1-pcm-render-input-v1`. The shared plan vocabulary is
`MaterialStart`, `MaterialTimeBase`, and
`normalized-by-render-material-v1`; Viewer-proxy names are not part of the
semantic schema. A producer manifest remains profile-specific and is validated
by its adapter before it may become a common plan input.

## Visual composition

Video track ordering defines composition order. The lowest enabled video track
is the base; higher enabled tracks composite over it. Track gaps are transparent
inside the plan and resolve against the preset background at final output.

Each visible source instruction names an exact product source stream and range, exact
timeline range, orientation normalization, crop, scale, translation, opacity,
and fit policy. The first slice has no hidden transition, speed change, tracking,
or effect evaluation.

The first coordinate policy is `oriented-crop-fit-anchor-canvas-v1`. It uses a
continuous oriented-source and canvas coordinate system whose pixel centers are
`x + 1/2, y + 1/2`. Crop and anchor use bounded basis points, scale is a positive
dimensionless `ExactRational`, and translation is an exact signed fraction of
canvas width or height. Crop boundaries are exact fractions of the complete
oriented material raster and are never rounded into a second integer crop. The
render-material producer already owns orientation normalization; this renderer
accepts only `normalized-by-render-material-v1` and never applies a second
rotation.

Evaluation order is crop, `contain|cover` fit, anchor-relative scale, then
translation. The crop anchor maps to the same basis-point location on the
canvas; extra scale keeps that point fixed, and translation is applied last.
Output pixel centers are inverse-mapped through that transform. Samples outside
the continuous crop rectangle are transparent rather than edge-extended.

The software reference compositor uses the closed policy set
`rec709-left-linear-rgba16-integer-v1`,
`pixel-center-mitchell-fixed-v1`, and
`source-over-half-even-v1`. It converts normalized Rec.709 proxy samples into a
fixed-point linear premultiplied RGBA16 workspace, applies the pinned Mitchell
kernel, and composites source-over with round-half-even arithmetic. Lower video
layers render first, higher layers cover them, captions render afterward, and
the declared background resolves remaining transparency. A host scaler, GPU
blend mode, chroma siting guess, or floating-point library default cannot
replace these policy identities.

The color reference oracle accepts only limited-range 8-bit Rec.709 proxy
samples. Left-center 4:2:0 reconstruction shares one chroma row across each
two-luma-row pair, preserves the even-column co-site, and uses an edge-clamped
round-half-even midpoint for odd columns. Output downsample uses a pinned
horizontal `[1 2 1]` triangle at each left co-site followed by a two-row box and
one round-half-even division. YCbCr/RGB matrices are exact rational integers.
The nonlinear Rec.709 transfer is a checked-in 12-bit-knot LUT with a fixed
SHA-256 and integer interpolation; runtime `pow`, host floating point, swscale,
GPU conversion, and platform color services are not reference behavior.

The Mitchell kernel is the separable Mitchell-Netravali kernel with exact
`B = C = 1/3`. Minification widens its support instead of silently reverting to
a fixed four-sample reconstruction. Per-output-coordinate weights are
round-half-even Q30 values. An inverse-mapped center outside the continuous crop
is transparent; a center inside it normalizes the retained in-crop samples to
sum exactly to Q30 unity, so an identity full-raster crop does not acquire a
dark border. Horizontal and vertical passes each round-half-even, clamp into
RGBA16, and clamp premultiplied color channels to alpha. A single axis
may touch at most 512 source samples, and the private execution budget charges
the exact derived tap work; exceeding either bound is
`render-resource-limit-exceeded`, never a lower-quality fallback. The evaluator
reuses one output-frame workspace and a bounded transformed-row cache rather
than materializing one full transformed frame per layer.

At any exact time, non-overlap rules guarantee at most one clip per video track.
Multiple tracks may overlap to support deterministic B-roll and picture overlays.

## Audio mix

Every enabled audio instruction names an exact source stream and range,
timeline range, channel mapping, and gain. Enabled tracks mix together; silence
fills gaps. The plan fixes output sample rate, channel layout, and clipping
policy rather than inheriting encoder defaults.

Clip link groups affect edit-operation scope only; they do not alter RenderPlan
evaluation. The first slice has no automatic ducking, loudness normalization,
time stretch, or hidden linked-clip behavior. Any later analysis may propose
explicit gain state but cannot change rendering by itself.

Gain is bounded to `-96000..24000` milli-dB and follows
`millidb-q31-integer-v1`. A checked 56-fraction-bit one-milli-dB step is
evaluated by round-half-even exponentiation and converted once to an `int64`
coefficient with 31 fractional bits; this representation deliberately permits
gain above unity through `+24 dB`. Each active stereo proxy sample is multiplied in Q31, all
tracks accumulate in a signed 64-bit sum, and
`int64-sum-final-s16-hard-limit-v1` rounds and saturates exactly once into
signed 16-bit PCM. The first policy adds no dither. Pinned libopus consumes that
PCM; per-track clipping, host float mixing, automatic loudness, and ambient
channel matrices are forbidden.

## Captions and text

Caption instructions contain exact ranges, canonical BCP-47 language, text,
style, and pinned font resource identity. `und` is explicit creative state;
neither the compiler nor renderer consults a host/UI locale. Caption tracks
render after video composition in stable track order.
System font lookup is not a deterministic export dependency; the first release
ships and references a bounded signed product font bundle. Its canonical
manifest fixes every font file, ordered fallback, declared coverage, and
aggregate digest under one RenderPlan resource identity.

`explicit-lines-clip-v1` is the Caption style's wrap/overflow policy: only
explicit U+000A boundaries create lines, automatic wrapping is forbidden, and
content outside the safe-width box is clipped. `explicit-lines-bottom-box-v1`
is the separate placement/layout policy that anchors those already-explicit
lines in the bottom safe-area box.

`explicit-lines-bottom-box-v1` treats U+000A as the only line separator and
expands every U+0009 into exactly four U+0020 scalars before segmentation;
leading, trailing, and consecutive U+000A therefore preserve empty logical
lines. U+200C ZWNJ and U+200D ZWJ remain valid shaping input, but every other
Unicode `Cc`/`Cf` scalar and U+2028/U+2029 is invalid. In particular, authored
bidi overrides and isolates cannot bypass renderer direction policy. The
evaluator performs no Unicode normalization or locale-dependent automatic
wrapping. Each line is shaped inside the declared safe-width box and clipped
at that box. The
payload-pinned, statically linked HarfBuzz, FreeType, and FriBidi closure shapes
each line with the Caption's explicit language, a pinned first-strong bidi base
direction, pinned Unicode data, and the bundle's declared fallback order. Go
owns line assembly, cluster fallback, placement, and composition; the native
closure only performs bidi, shaping, outline loading, and grayscale raster.
Hinting and subpixel rendering are disabled; grayscale glyph
coverage is composited through `harfbuzz-freetype-fribidi-gray-v1`. Missing
glyphs and unsupported color emoji fail with typed diagnostics rather than
tofu, locale fallback, or a system emoji font.

The ABI is stateless but batched: bidi is one call per explicit line, coverage
is grouped by face and direction, shape requests carry the complete face-run
set, bounds carry the complete glyph set, and raster calls retain visual order
while batching adjacent equal-face glyphs within the byte cap. Each call opens
only its required in-memory pinned font bytes and destroys all FreeType,
HarfBuzz, and FriBidi state before returning. This avoids both persistent native
pointers and per-glyph TTC parsing. Outline raster uses an outside FreeType
stroke with round cap and round join; fill is composited afterward.

The first payload resource is `open-cut-caption-font-v1` version
`noto-sans-static-v1`, a fixed Noto Sans multilingual fallback bundle. It pins
Noto Sans regular Latin/Greek/Cyrillic 2.015; Arabic 2.013; Hebrew 3.001;
Devanagari 2.006; Bengali 3.011; Gujarati 2.106; Gurmukhi 2.004; Kannada 2.006;
Malayalam 2.104; Tamil 2.004; Telugu 2.005; Thai 2.002; Lao 2.003; Khmer 2.004;
Myanmar 2.107; and Noto Sans CJK 2.004. Only one declared regular TTF per
non-CJK source and `NotoSansCJK-Regular.ttc` enter the payload. Exact file
digests make their cmaps the coverage truth; the manifest declares face/index
and script roles rather than maintaining a second hand-authored code-point
range truth.

The TTC face indices are fixed as JP `0`, KR `1`, SC `2`, TC `3`, and HK `4`.
`bcp47-cjk-region-script-v1` uses the Caption's parsed BCP-47 fields: `ja`,
`ko`, and `yue` prefer JP, KR, and HK; for `zh`, HK/MO region wins first,
followed by Hant/TW, Hans/CN/SG, then bare-`zh` SC. The preferred face is tried
before the bundle fallback order. Fallback is evaluated at an
extended-grapheme-cluster boundary: the first declared face that can shape the
complete cluster wins, and a cluster is never split merely because separate
fonts cover separate scalars. Color emoji, unsupported variation sequences,
and any cluster absent from every pinned face fail with a typed glyph
diagnostic. Runtime locale, font discovery, and visually similar substitution
are forbidden.

`unicode-egc-15.0.0-uniseg-v0.4.7` is the first EGC data identity. The generated
Unicode 15.0.0 tables from pinned `uniseg` v0.4.7 are compiled into the renderer
helper and covered by its source/binary closure; they are not a host Go/OS table
or a separately mutable runtime file. `first-strong-ltr-fallback-v1` applies
independently to every explicit line: the first strong scalar fixes its base
direction and a line with no strong scalar is LTR. Caption language informs
HarfBuzz shaping and CJK face preference, never this bidi fallback. FriBidi
returns a bounded visual partition over Go-supplied EGC boundaries; Go rejects
gaps, overlaps, or an incomplete partition. Within each visual bidi run, Go
selects one face per complete EGC, coalesces adjacent equal-face clusters,
reverses those face segments for an odd embedding level, and then asks HarfBuzz
to shape each logical segment. A zero glyph or an over-bound glyph expansion is
a typed failure, never a scalar-level fallback.

Narrative authored-text and visual-intent nodes do not render merely because
they exist. A transaction must create Caption or other future executable
Sequence entities.

## Preview

Sequence preview compiles the same plan schema as export but may select compatible
proxy inputs and a lower-cost output policy. It must preserve exact timing,
track order, reframe, gain, caption, enabled state, and missing-input diagnostics.

The UI renderer may evaluate plan instructions incrementally. It cannot invent
state absent from the plan or silently choose a different source range. A plan
or proxy mismatch falls back to another compatible pinned input or surfaces
typed degraded state.

The first Sequence preview profile is
`webm-vp9-opus-sequence-preview-v1`. It fixes a maximum 1280-pixel long edge
without upscaling, even square-pixel dimensions, opaque black background,
SDR Rec.709 `yuv420p`, pinned single-thread libvpx-vp9 settings, and 48 kHz
stereo pinned libopus settings. It uses the complete first-slice RenderPlan
semantics: bounded multi-Clip composition, multi-track overlay and mix, gaps,
reframe, gain, and captions. A singleton A/V implementation fixture is not a
smaller registered capability.

The output uses left chroma siting, a signed-16-bit no-dither Opus input path,
and `frame-zero-two-second-grid-v1`: frame zero is a keyframe and the first
Sequence frame on or after each two-second boundary is a keyframe, with scene
cut disabled. `webm-bitexact-no-segmentuid-v1` fixes track order,
timecode/cluster policy, metadata removal, bitexact output-muxer scope, and
omission of the optional random Segment UID. `exact-sample-count-discard-padding-v1` fixes Opus codec delay and
end discard padding so decoding yields exactly the plan's audio sample count.
Container timestamp rounding remains transport data and never feeds creative
time.

Every non-empty Sequence preview contains exactly one CFR video stream and one
stereo audio stream. Audio-only content receives black video, video-only
content receives silence, caption-only content receives black video and
silence, and internal gaps receive black or silence. A Sequence with no
positive-duration enabled Clip or live Caption returns `empty` and creates no
plan, job, or artifact.

### Exact output grid

The plan keeps exact rational `semanticDuration` derived from committed enabled
content. Physical output uses explicit half-open sampling grids:

```text
videoFrameCount  = ceil(semanticDuration * sequenceFrameRate)
audioSampleCount = ceil(semanticDuration * 48000)
```

Video is sampled at Sequence frame boundaries and audio at output sample
boundaries. A Clip or Caption is active when the sample time is inside its
half-open timeline range. The final partial frame/sample interval is padded
with black or silence and never truncates creative content. Frame count, sample
count, stream-shape policy, sampling policy, and tail-padding policy are fully
expanded in the RenderPlan digest. Artifact facts record both semantic duration
and the resulting presentation duration.

Every video input pins both its source and proxy time bases plus the exact
source/proxy PTS-map digest. For an active output frame, the evaluator maps the
Sequence frame time into exact source time and selects the greatest mapped
source PTS not later than that time; before the first mapped PTS it uses the
first frame. Proxy container PTS rounding is transport data and cannot replace
this `source-map-floor-first-fallback-v1` rule.

The evaluator opens the fixed-record `OCPMAP01` file only after streaming its
complete header, count, strict PTS ordering, length, and SHA-256 validation. It
keeps no frame-sized or map-sized in-memory index: the first target of a run is
selected by bounded random-access binary search and later monotonic targets
advance one cursor. A decode run starts at ordinal zero unless a future seek
optimization proves the exact starting ordinal; its resource charge is the
number of source frames actually traversed. Starting another run charges its
traversal again, and a backward ordinal inside one run is invalid. This is
`source-map-binary-monotonic-cursor-v1`; it preserves the semantic selection
rule while making map memory O(1).

`monotonic-lanes-ordinal-zero-v1` assigns frame-producing instructions to at
most the admitted peak active-video lane count. Timeline-overlapping
instructions use different lanes. A freed lane continues its current decoder
run only when the next instruction uses the same exact input/stream and its
first selected ordinal is not behind the run's last ordinal; otherwise it
starts a new ordinal-zero run and charges that traversal again. Compatible
lanes prefer the greatest already-decoded ordinal, with stable lane index as
the tie break. Each live lane retains at most one compact source `yuv420p`
frame; complete decoded clips are never cached.

Every audio input likewise pins source/material time bases, the normalized 48-kHz
material epoch, and the exact decoded material sample count. The first valid sample
after Opus pre-skip is track-relative ordinal zero; a raw PCM pipe does not
invent silence between the common material epoch and a later track start. For
output sample `n`, the evaluator computes exact values:

```text
sourceTime = SourceRange.start + n / 48000 - TimelineRange.start
materialTime = MaterialStart + sourceTime - SourceStart
ordinal    = floor((materialTime - MaterialStart) * 48000)
           = floor((sourceTime - SourceStart) * 48000)
```

An active instruction before `SourceStart` produces silence. An active request
at or beyond `decodedSampleCount` is `render-source-range-invalid`; it is not
silently padded. Samples outside active Clips are silence. This is the
`render-material-sample-floor-silence-v1` rule; encoder timestamps or resampler defaults
cannot choose a different boundary.

`monotonic-s16-lanes-ordinal-zero-v1` applies the video lane rules to audio:
timeline overlap uses separate lanes, a freed lane continues only for the same
exact input/stream and non-backward ordinal, and every backward traversal starts
a new ordinal-zero run and is charged again. A live lane retains at most one
4,800-sample interleaved stereo S16LE chunk. The pinned child explicitly uses
fixed-point libopus/S16 and no seek, filter, resample, channel mapping, gain, or
mix option. After the declared final ordinal the contained child is
intentionally terminated because FFmpeg's audio frame limit counts codec
frames, not product samples; early EOF remains a failure.

The evaluator admits at most 32 simultaneously active audio lanes. This is a
physical contained-decoder bound, not a limit on total Project tracks. It emits
at most 4,800 stereo samples per backpressured block, starts decoder runs lazily,
keeps compatible runs alive across gaps, and closes each run immediately after
its declared last ordinal. Video and audio evaluation are separate pipeline
phases so their decoder-process peaks do not stack. Raising the active-audio
limit requires a new in-process decode or staged mixing architecture; changing
only the constant is not conformant.

Caption activity is evaluated against the integer output-frame index derived
from its exact half-open range. Subtitle-container timestamp precision never
quantizes a caption boundary.

Caption font size, outline width, and vertical position are basis points of the
output canvas height; safe width is basis points of canvas width; line height is
a basis-point multiplier of font size. These values become round-half-even 26.6
fixed-point coordinates. `positionY` is the lower edge of the bottom line's
primary-font em box. The primary face is the first language-aware face in the
pinned order. Its signed descender is scaled from font units by round-half-even,
and `baselineY = positionY + scaledDescender`; earlier lines move upward by the
exact line-height step. Fallback-face metrics never move the baseline, preventing
text-dependent vertical jitter. Each line is centered in the 26.6 safe-width
box even when it overflows. Safe-box clipping admits pixels by their centers;
vertical clipping uses the output canvas. Text and outline coverage are tight
grayscale bitmaps, not full-frame layers. An all-empty or all-whitespace caption
is an explicit zero-geometry active layer. The first evaluator rasterizes once
when an instruction becomes active, retains that immutable coverage only until
its half-open end, and frees it immediately; it has no cross-instruction cache.
Coverage overlap uses round-half-even 8-bit source-over. Caption `#rrggbbaa`
RGB bytes are full-range nonlinear Rec.709 and enter the same pinned linear LUT
as video before outline-then-fill composition.

`open-cut-render` is the only native-text caller. A narrow in-process C ABI
statically binds the pinned HarfBuzz, FreeType, and FriBidi closure; fixed-width
values and bounded byte buffers cross the ABI, and native pointers never survive
a call. Go retains ownership of explicit lines, pinned extended-grapheme
segmentation, fallback selection, placement, and composition. Grapheme property
data is an embedded, versioned generated renderer closure resource rather than
ambient Go or OS Unicode state. Native bidi output is capped by input EGC count;
shape output is capped at eight glyphs per input scalar plus sixteen; and each
glyph raster call is capped at 16 MiB before a coverage buffer is allocated.
A native fault terminates only the contained renderer helper and
is reported as `renderer-process-failed`; it does not add another tool, Agent
entry, PATH command, or runtime-topology process.

The API starts the helper with the active application lifecycle profile, but
that profile is operational process policy and never enters RenderPlan or the
private execution digest. Inside the helper, every FFmpeg decoder/encoder/muxer
child uses the fixed `packaged`, headless, contained-tree lifecycle policy on
all surfaces, including development and harness execution. No environment
variable or execution field may select a weaker media-child profile.

### Durable preparation and publication

Sequence preview uses one generic WorkJob with typed sequence-preview details;
there is no second PreviewRequest state machine. Its immutable intent pins the
Project and Sequence identities, expected Sequence revision, output profile,
dependency resolver, compiler, and renderer identities. Before the job may be
claimed, preparation fixes each SourceStream/resource requirement to an exact
producer WorkJob and holds typed prerequisites for their immutable results.

Job creation also persists one canonical `SequencePreviewRenderIntent` snapshot.
It contains the exact render-relevant Sequence format, ordered Track identities
and order keys, active Clip and Caption states, Asset revisions and accepted
fingerprints, plus the exact producer requirement for every Clip. Display names,
labels, LinkGroups, tombstones, disabled Clips, and other edit-only projection
state are excluded. This snapshot is the only input used to compile an unbound
job after its prerequisites become ready; current Project or Sequence
projections are never consulted to reconstruct an older revision.

The first claimed attempt atomically compiles and publishes the RenderPlan and
binds its digest to the typed job details before launching the renderer. A
retry reuses that plan. The Creator preparation API is a projection of this
durable job; UI polling, SSE, or lease renewal never drives business progress.

Success atomically publishes a typed `SequencePreviewArtifact` separate from
Asset-owned `MediaArtifact`. It records plan digest, renderer build/target
identity, profile, verified media facts, content digest, byte size, and product
data reference. RenderPlan digest describes semantics; content digest describes
bytes. Cross-target byte identity is not promised, so renderer build/target is
part of job identity. No successful artifact is overwritten.

Within one renderer build/target, equal plan and immutable input bytes must
produce equal output bytes under `same-build-target-byte-stable-v1`. A retry
that produces a different digest fails as `renderer-nondeterministic`; it does
not publish a second equally valid result. Cross-target output bytes may differ
only within the shared semantic conformance contract.

The target name is not permission to inherit a machine's optional CPU feature
set. The initial software closure disables runtime codec CPU dispatch and fixes
an architecture baseline: ARM64 permits baseline NEON but not dot-product,
I8MM, SVE, or SVE2; x64 permits its architecture baseline but not SSE3, SSSE3,
SSE4.1, AVX, AVX2, or AVX-512. Renderer FFmpeg children additionally clear
runtime CPU flags. A later optimized build requires a distinct closure identity
and its own conformance evidence.

Preview bytes are a derived cache. A live lease, active job, or explicit pin
prevents eviction; otherwise a project cache policy may evict bytes while
retaining metadata. Rematerialization restores the same Artifact ID only when
the bytes match the recorded digest. A compiler, renderer, profile, or target
change creates a new job and artifact.

The first delivery does not silently evict ready preview artifacts by capacity;
it removes attempt garbage, orphan publication directories, and verified
corruption only. A later creator-visible storage policy may evict unleased
derived bytes without changing artifact identity. Private range/segment caches
may optimize rendering behind the same immutable RenderPlan and final artifact,
but never become creative truth, Viewer DTOs, or Agent commands.

The registered renderer is one API-local, payload-pinned capability that
evaluates the complete plan; it is not a caller-supplied FFmpeg command or a
second UI composition model. Its Go-owned compiler emits direct argv and
bounded renderer inputs only. The capability remains unregistered unless its
conformance suite covers VFR source-map boundaries, multiple Clips and tracks,
overlay/mix, gaps, reframe, gain, captions, audio-only, video-only,
caption-only, and non-grid tail padding. Passing a singleton A/V fixture does
not advertise the capability.

The first profile applies `sequence-preview-bounded-streams-v1` before a helper
starts. The API derives and the helper revalidates the same private budget: at
most 32 simultaneously active video layers, 32 audio layers, and 64 caption
layers; at most `2^43` conservative pixel-samples and `2^38` mixed
audio-samples; at most 16 MiB expanded caption UTF-8, `2^18` explicit caption
lines, `2^20` EGCs, and 256 MiB conservative peak live caption coverage;
bounded resample-tap work and decoder traversal, 16 GiB per final/intermediate file,
48 GiB for the complete attempt, a one-frame video chunk, and a 4,800-sample
audio chunk. The API also derives a duration-scaled scratch admission floor and
checks the attempt filesystem before launch. A semantic plan beyond this
profile fails as `render-resource-limit-exceeded`; insufficient real storage
fails separately as `render-storage-insufficient`. OOM, ENOSPC, or timeout is
never the intended admission oracle, and no instruction is silently dropped to
fit the profile.

Conformance has two layers. Pack/release qualification runs the complete
semantic matrix and renders every byte-stability fixture at least twice on one
build/target; unequal output digests reject that capability. Cross-target
qualification compares exact plan results, selected source PTS, frame/sample
counts, pixel/audio reference oracles, and verified media facts, but does not
require equal encoded bytes. The signed catalog records the conformance profile
and exact suite/evidence identity. Evidence is a contained, digest-bound notice
whose target, dependency pins, semantic observations, and repeated byte digest
must reproduce during target-local qualification. A content-addressed receipt
may carry that successful expensive qualification across packaging runs only
for the exact same closure and contract; deployed renderer matrix smoke still
executes and any receipt mismatch replays the full owner check. API startup
only verifies the authenticated catalog and exact contained byte closure; it
does not execute the helper, relink native inputs, or place any conformance
fixture on the cold start path. Bounded smoke execution belongs to build, check, pack, release, and
post-install delivery-harness qualification; the user installer only verifies
the signed manifest and contained bytes.

The first implementation is a dedicated Go `open-cut-render` executable in
the API artifact closure. The API resolves it from the verified media catalog
and gives it one private, bounded execution manifest plus contained
attempt/output paths. That manifest pins the exact entry tool, raw
decoder/encoder tools, product-font resource, and their closure digests.

The private helper has one closed result protocol. It atomically writes
`result.json` beside the execution manifest: success binds the relative output
path, exact byte size, and SHA-256, plus the exact byte counts and SHA-256 of
the evaluator's raw YUV and S16 streams before encoding; failure carries one
closed diagnostic code and optional typed subject. These raw observations are
always produced by the normal backpressured streams, not by a conformance-only
entry or a second render pass. They separate semantic identity from target-local
encoded-byte identity during qualification. The API accepts typed semantic failure only when the
helper exits unsuccessfully with a valid failure result. A crash, timeout,
missing/invalid result, successful exit with failure content, or failed exit
with success content is `renderer-process-failed` or `renderer-output-invalid`.
stdout is never a protocol and stderr remains bounded human diagnostics.
The helper accepts exactly `--execution <absolute execution.json>`, rejects a
linked or non-canonical execution path, and never overwrites an existing result
or output. Font/resource-limit/missing-glyph/color-glyph failures retain their
typed identity even when a downstream pipe also exits; ambiguous child
failures become closed encode/internal failures rather than being inferred from
stderr text.

Pinned FFmpeg subprocesses are byte-stream adapters only: they decode declared
proxy streams to bounded raw frames/PCM and encode the evaluator's explicit CFR
frames/PCM with closed settings. The Go evaluator owns source-map selection,
sampling grids, crop/reframe, fixed-point composition, gain/mix, captions,
gaps, and tail padding. FFmpeg filters, timestamps, auto mapping, resampling,
mixing, scaling, or subtitle rendering cannot decide product semantics. The
helper is not installed as the product CLI, added to `PATH`, exposed in
runtime topology, or invocable through an Agent command.

The encoder path never materializes complete raw video or PCM. Go feeds one
bounded, backpressured stdin stream at a time: CFR `yuv420p` frames produce a
video-only compressed intermediate and stereo S16 PCM produces an audio-only
compressed intermediate. A final bitexact stream-copy mux produces
`preview.webm`, after which intermediates are removed. Each stream rejects a
write before a frame/sample chunk would cross its manifest limit; each output
uses an independent file ceiling and the attempt uses an aggregate ceiling.
Compressed intermediates are operational attempt data, not RenderPlan inputs,
cache identities, or publishable artifacts.

The expected renderer version and target come from that verified payload
catalog entry and its content/build closure, never an API source constant. The
Creator preparation route and scheduler executor become available together only
after the declared renderer bytes and complete media-tool closure verify. A
declared but corrupt or incomplete capability therefore fails closed instead of
accepting jobs that no executor can claim.

An attempt writes only below its API-datadir work directory. Publication
verifies media facts and full digests, fsyncs files, atomically renames the
artifact directory under `artifacts/sequence-preview/<artifactId>`, fsyncs the
parent, then commits the artifact row, typed job result, successful attempt,
and activity in one SQLite transaction. A crash before the transaction may
leave only an unreferenced directory, which cold-start reconciliation removes;
a committed row can never name bytes that were not durably renamed first.

## Export

The public lifecycle, full-quality render-input boundary, first exact preset,
artifact projection, and recovery rules are normative in `specs/export.md`.

Export freezes the normalized plan digest before rendering starts. Its logical
preset expands into explicit canvas, frame rate, pixel format, color policy,
audio layout, codec settings, and container settings; encoder defaults are never
part of the product contract.

The pinned payload media toolchain executes the plan into a temporary
project-owned output. Success requires atomic publication plus verification of
container, streams, exact expected duration tolerance, dimensions, frame rate,
audio format, and non-empty bytes. The immutable ExportArtifact records the plan
digest, preset, toolchain version, facts, and content digest.

Hardware acceleration is an execution choice behind the same plan and preset.
If a platform path cannot honor declared semantics, it falls back or fails with
a typed diagnostic; it never rewrites the plan.

### First closed preset

The sole initially registered preset is `webm-vp9-opus-v1`:

- seekable WebM container;
- VP9 video with an explicit pinned software encoder profile and settings;
- constant frame rate equal to the Sequence rational frame rate;
- Sequence canvas dimensions with square pixels;
- `yuv420p` output;
- SDR Rec.709 color policy;
- Opus audio at 48 kHz stereo with explicit channel mapping;
- transparent composition resolved against opaque black;
- caption text rendered through pinned product font resources.

The first preset accepts only the qualified SDR Rec.709 render-input profile.
HDR/PQ/HLG/Dolby Vision remain typed unsupported until a separately pinned
software reference tone-map and conformance capability exists. A hardware
decode or encode path is permitted only when verification proves output facts
and declared color semantics equivalent; otherwise export uses the pinned
software path or fails. The preset never inherits host codec, color, audio, or
quality defaults.

The preset ID is semantic and immutable within its schema version. Changing a
codec, pixel format, color transform, audio layout, or material quality setting
requires another preset version.

`social-mp4-v1` remains a reserved compatibility target, not a baseline
capability. A release registers it only when an API-local H.264/AAC encoder
module, its exact profile, patent/license review for the release markets, and
the full conformance fixture are present. A missing module never falls back to
libx264, an OS encoder, or another preset.

## Missing and changed inputs

Compilation rejects an input whose current accepted fingerprint cannot satisfy
the pinned Asset identity. A source disappearing after compilation fails the
ExportJob with the affected Asset and instruction identities.

Preview may remain degraded using an already compatible pinned proxy when the
original is missing or changed. When no proxy exists but the accepted source is
readable, preparation creates or reuses the exact SourceStream proxy job. When
the source/resource is unavailable or unsupported and no compatible artifact
exists, the current WorkJob fails terminally with typed diagnostics; relink,
resource installation, or capability change creates an explicit retry/new job.
Export uses only artifact kinds explicitly permitted by its preset and never
substitutes an unrelated or stale artifact.

## Invariants

- Preview and export never interpret Sequence state through separate semantic
  models.
- A RenderPlan never mutates Project or operational state.
- No encoder, browser, OS, or hardware default becomes hidden creative state.
- No system font or system media binary determines verified output.
- No export can observe a moving Sequence head or active-artifact selection.
- A plan digest and its pinned inputs are sufficient to explain rendered output.
- No UI poll or lease renewal is required to advance durable preview work.
- No valid first-slice plan is silently narrowed to a singleton A/V renderer.
