# Release bundle contract

Status: Day 0 baseline.

The activation and rollback unit is `release-bundle.tar.zst`:

```text
manifest.json
launcher/<platform pack>
payload/<opaque full application pack>
```

For Open Cut the payload is assembled from `apps/web`, `apps/api`, and
`apps/electron` into a full Electron pack. That source graph is build-plane
knowledge and is absent from launcher state and release manifests.

`oc-control pack` discovers every `apps/*/sidecar/manifest.json`, including the
carrier, validates each declared compiled artifact, production-deploys each app
independently, executes its bounded app-relative native `artifactChecks`, and
writes one generated `runtime-topology.json`. A manifest may select the generic
`$node` or `$payload` token, or an app-relative native command. Artifact checks
remain build-plane gates and are omitted from topology. The shared Go runtime
runner consumes the resolved topology; there is no second hand-maintained list
of app entries and Electron never consumes or owns the topology.

The opaque payload currently contains this carrier-owned shape (platform wrapper
details omitted):

```text
payload/runtime-topology.json             # sole opaque payload entry
payload/bin/open-cut[.exe]                 # versioned product CLI
payload/resources/agent/<prompt contract>  # versioned shipped Agent prompt
payload/app/
  <Electron executable and frameworks>
  resources/app/                          # Electron app source + sidecar entry
  resources/payload/sidecars/<app>/
    dist/sidecar/<declared artifact>       # JS or native app sidecar artifact
    node_modules/                          # app-scoped production dependencies
  resources/payload/sidecars/api/
    dist/sidecar/api-sidecar.exe
    dist/sidecar/media-tools.json          # API-local closed tool registry
    dist/sidecar/product-resources.json    # authenticated optional-resource catalog
    dist/sidecar/media/<platform tools>    # pinned probe/decode/render/transcribe tools
    dist/sidecar/media/resources/          # small qualification-only resources
    dist/sidecar/licenses/media/           # notices/source-build metadata
```

The media toolchain is deliberately inside the API sidecar artifact closure,
not a global payload bin directory. The API resolves its physical executable
and then validates only contained manifest-relative tools. The runtime topology
does not carry media paths or capabilities, the sidecar launch contract does
not grow a product resource field, and API data-directory derivation remains
unchanged.

Each media build recipe fixes a public-target CPU baseline and disables ambient
runtime codec dispatch. Optional host instructions cannot vary bytes beneath a
single target/recipe identity; any optimized feature profile is a new verified
closure with new conformance evidence.

The first local transcription closure pins `whisper.cpp` 1.8.6, builds a static
CPU-only `whisper-cli` at the public target's minimum CPU baseline, and binds it
with the contained FFmpeg/FFprobe tools, MIT notice, normalized recipe, and a
small source-contained test model. That test model exists only to qualify and
replay engine behavior; the multilingual `small` production model remains an
on-demand ProductResource and is never copied into the release payload.

The audio baseline builds libopus fixed-point with assembly, RTCD, and
intrinsics disabled. FFmpeg retains the libopus float symbols only for static
link compatibility; renderer decode selects `libopus` with requested S16
samples. The contained tool enables `pcm_s16le` and the raw `s16le` muxer only
as private byte adapters. Cross-target qualification compares the exact decoded
sample count and PCM digest; encoded WebM byte equality remains a same-target
requirement.

Optional business executors such as `open-cut-render` are advertised only by a
closed capability record in this release-authenticated media catalog. Catalog
schema v2 separates executable tools, non-executable resources, and
capabilities. A capability names one entry tool, its complete sorted
tool/resource dependency set, one conformance profile, the exact suite digest,
one contained conformance-evidence notice, and an aggregate closure digest. The
evidence notice fixes target, exact tool/resource dependency digests, sorted
semantic observations, and byte-stability observations required by that
profile. A font directory, co-located encoder, evidence file, or legal notice is
never inferred from sibling layout.

At API startup the record, target, every declared byte, notices, and aggregate
closure digest are verified together. The exact renderer job identity is
derived from the catalog version, target, capability closure digest, and
build-recipe digest; application source constants describe only protocol
compatibility and cannot assert that a payload build exists. The route,
expected renderer identity, and scheduler executor are installed as one
decision only after the whole closure verifies.
Pack/release qualification reruns the declared suite and requires its observed
evidence to equal the catalog notice. A suite implementation change therefore
changes its suite digest and cannot silently reuse old evidence.

The first renderer closure carries `open-cut-caption-font-v1` as a mandatory
payload resource rather than an on-demand ProductResource. Its canonical file
list fixes regular contained font files, file/face indices, language-selection
policy, script roles, fallback order, licenses, sizes, individual digests, and
one aggregate resource digest. Exact font bytes fix Unicode cmap coverage; no
parallel manually curated range list can drift from those bytes. It is
available offline with the renderer, never resolved through a system font
directory, environment variable, or data-root convention.
The catalog builder and renderer runtime use one minimal shared canonical
resource-closure digest primitive. Runtime font verification belongs to the
render engine and imports no downloader, archive extractor, compiler driver, or
other build-plane package. It verifies the exact symlink-free directory tree,
bundle manifest, every file size/digest, and aggregate catalog-relative
resource digest before native code receives font bytes.

The native text closure pins HarfBuzz 14.2.1, FreeType 2.14.3, and FriBidi
1.0.16 source archives. Builds use only static outputs with ambient optional
font libraries disabled. A release that statically links an LGPL component
must include the applicable corresponding source, object/relink material, and
rebuild instructions in the authenticated closure; license notices or source
URLs alone do not satisfy this gate. `sequence-preview-renderer-v1` is present
only when that material and the renderer conformance evidence both verify; a
development payload may still carry the private helper/font bytes while
omitting the capability if either gate is incomplete. The production
`desktop-creator-v1` artifact check rejects such a reduced catalog: probe,
frame inspection, source proxy, and the complete Sequence renderer must all be
registered and qualified before packaging may publish that target.

The Go side of the same renderer closure pins `uniseg` v0.4.7 and its generated
Unicode 15.0.0 segmentation tables under
`unicode-egc-15.0.0-uniseg-v0.4.7`. The helper binary, exact module source and
checksum, and MIT notice are authenticated together. No runtime Unicode data
file, host standard-library table, locale pack, or independent download may
change EGC boundaries for an already identified renderer.

The renderer links those libraries only through its bounded private C ABI. Its
authenticated target closure includes the exact corresponding source,
non-library target link inputs, a normalized link manifest, and rebuild/relink
instructions. `media-tools check` must perform a real relink and execute the
resulting bounded renderer smoke fixture; checking that material merely exists
is insufficient. Baseline and modified-library relinks each use a fresh
kit-owned Go build cache; ambient cache entries cannot qualify corresponding
source, and generated cache bytes are removed before the relink kit is archived.
The relink output need not equal the release binary byte for byte, but it must
satisfy the same ABI and semantic smoke observations. No shell wrapper becomes
a second development or operations entry point.

The transcription engine follows the same API artifact-closure ownership as
other business executors. It is never a global payload binary, launcher tool,
PATH entry, sidecar, or runtime-topology capability.

On-demand transcription models are ProductResources below product data, not
release payload binaries. The catalog is adjacent to the physical API executable
inside the API sidecar artifact closure, and is authenticated transitively as
immutable content of the signed release. Each target-specific payload catalog
declares the logical resource name, kind, version, compatibility profile,
origin, byte size, SHA-256, and retention policy. API acquisition accepts only
the canonical exact catalog entry and verifies bytes before publication.

The first release introduces no second resource trust root or rotation state. A
catalog change requires a new signed release; already acquired resources remain
usable only when the active catalog declares them compatible. Acquisition and
retention remain product business behavior rather than launcher activation state.

The generated topology contains only generic process descriptors: app subject,
relative command, arguments, working directory, environment additions, and
environment removals. Platform-specific pack adapters may encode Electron or
Node invocation details into those values. Launcher code only validates and
executes the descriptors; it has no carrier-specific branch.

The packer preserves only relative symlinks that remain inside the full-pack
root. A `pnpm deploy` self-reference that points back to the source workspace is
recognized by exact package name and removed; any other escaping link rejects the
build. The resulting release cannot depend on the build machine's checkout.

Canonical versions strictly match `X.Y.Z-<channel>.N`, including stable versions
such as `1.2.3-stable.4`. A display projection may render stable as `1.2.3`, but
display values never drive signatures, paths, ordering, state, or logs.

## Detached verification

Compressed-byte digest and signature metadata must be outside the archive to
avoid a circular self-digest:

```text
metadata/root.json
metadata/<channel>/<platform>-<arch>/latest.json
releases/<version>/<platform>-<arch>/release.json
releases/<version>/<platform>-<arch>/open-cut-<version>-<platform>-<arch>.release-bundle.tar.zst
```

`release.json` is a signed envelope containing schema, channel, canonical
version, public platform, public architecture, bundle size, SHA-256,
origin-relative bundle path, minimum bootstrap protocol, and publication time.
`manifest.json` contains launcher and payload entries plus the extracted tree contract.

The only public target values are `mac|win|linux` and `arm64|x64`; internal Go
values (`darwin`, `windows`, `amd64`) are adapter details. Immutable release
metadata and bundle bytes are target-scoped. Re-running publication for the same
content preserves the existing immutable `release.json`; conflicting content is
rejected.

B0 verifies trusted metadata and compressed bytes before extraction. Extraction
is implemented in Go, rejects absolute/traversing paths, special files, and
escaping links, and stages under `incoming/<transaction-id>` on the same
filesystem as `versions/`. Promotion is one rename.

Ed25519 trust roots are injected at bootstrap and persisted as local root version
1 before the first network update. `metadata/root.json` is an envelope containing
the next root. Rotation must advance exactly one version and meet the current
root's distinct-key signature threshold; the new root cannot authorize itself.
Downgrades, version gaps, invalid thresholds, duplicate key IDs, and malformed
keys are rejected. Release metadata is verified with the persisted root after
any accepted rotation. Unknown non-critical fields are additive; unsupported
critical fields reject only the candidate and keep last-good bootable.

Day 0 development macOS packs are ad-hoc signed after electron-builder produces
the full directory. Distribution identities, notarization, Windows signing, and
Linux repository credentials remain injected pipeline concerns and are not
launcher state.

Packaging verification also proves that the pinned media toolchain can execute
every registered export preset on the target. Codec distribution and
license compliance are release gates; a missing compliant encoder cannot silently
change preset semantics.

Media packaging additionally rejects GPL/nonfree FFmpeg configuration,
validates the source/build/license record and every declared executable digest,
runs the closed capability conformance fixtures, and bundles the exact
corresponding-source reference. No packaging or runtime path downloads a
prebuilt media executable.

Renderer qualification is target-local. Each release-target runner builds the
native helper and its dependency closure, performs the relink smoke, generates
the semantic/repeated-byte evidence, and packages those exact bytes on that
target. A runner may compare semantic observations with another target, but it
must never copy another target's helper, relink record, or conformance evidence
into its own catalog.

The initial `probe-v1` conformance gate generates its fixture from bounded code,
not a committed media binary or ambient encoder. It requires the packaged probe
to identify one exact 1-second AVI containing 16x16 raw video and 8 kHz stereo
PCM, then requires rejection of a truncated RIFF. The same gate runs when a
source build is first staged, whenever a validated build is reused, and from the
deployed app-relative artifact check during packaging.
