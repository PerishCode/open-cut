# Viewer media delivery

Status: Business design baseline.

## Boundary

The creator Viewer consumes product-owned media through Contracts and a
same-origin transport lease. It never receives an original path, API data path,
SourceGrant, filesystem bookmark, internal sidecar endpoint, or unrestricted
artifact URL.

This UI transport is separate from the Agent's bounded `asset frames` result.
The Agent cannot request, renew, or fetch Viewer MediaLeases; its only image path
remains a read-only leased copy in active turn scratch returned by the CLI.

## MediaLease

Contracts exposes a typed use case that returns:

```text
MediaLease
  schema
  resourceId
  purpose: source-preview | sequence-preview | waveform | thumbnail
  artifactId
  artifactDigest
  mimeType
  byteLength
  etag
  sameOriginUrl
  expiresAt
  pinnedProjectRevision?
  pinnedSequenceRevision?
```

Preparation is a separate typed envelope. Non-ready results carry a closed
`stage` (`proxy | integrity | render`) and bounded diagnostics with code,
severity, subject kind/ID, and a closed recovery action. The first recovery set
is `automatic-retry | retry-job | relink-source | acquire-resource |
adopt-revision | update-runtime | none`; a lossy `retryable` boolean is not a
contract. Diagnostics are never free-form display strings and never hide
whether the blocking subject is a Job, Artifact, or Asset.

Sequence preparation is Creator-only and requires Project ID, Sequence ID, and
an exact expected Sequence revision. Its closed result is
`empty | preparing | ready | failed`. `preparing` references the durable typed
WorkJob and its current stage; `ready` contains a lease for the WorkJob's pinned
RenderPlan and SequencePreviewArtifact. A ready result may carry bounded
non-blocking diagnostics, such as using a compatible proxy while the original
source is offline, without changing its artifact identity.

Every non-empty result returns an opaque continuation containing the exact
WorkJob ID and, once bound, its RenderPlan digest. Subsequent polling and renewal
must return that continuation with the same Project, Sequence, and Sequence
revision. Continuation follows an integrity-repair retry chain to its current
tail but never performs a new moving-head preparation. `empty` has no
continuation.

The request is a closed operation rather than an optional-field convention:

```text
prepare  -> exact expected Sequence revision, no continuation
continue -> exact expected Sequence revision plus continuation
retry    -> exact expected Sequence revision plus terminal continuation
```

`continue` is observational apart from automatic integrity repair. `retry`
creates a new `retry_of` WorkJob from the stored render intent and never reads
the moving Sequence head. If the predecessor already bound a RenderPlan, the
child reuses it. Calling `prepare` is the only way to begin work for a newly
adopted revision.

The URL is opaque, short-lived, purpose-bound, first-party-session-bound, and
valid only for the current API/Web/Electron process chain. It contains no path,
grant, stable capability, or guessable artifact location. Contracts returns it
only to the Viewer adapter, not the general Project model.

The lease pins an immutable artifact or normalized preview manifest. Motion
preview never streams original container bytes through the browser chain. A
source preview uses a normalized immutable proxy; while it is unavailable the
port returns typed `preparing` state plus a bounded normalized still if one is
ready. SourceAccess authority never becomes a Web/Electron streaming grant.

The first source-preview lease accepts Project ID, Asset ID, the exact expected
Asset revision/fingerprint, one explicit video SourceStream ID and/or one
explicit audio SourceStream ID, and the closed source-preview purpose. It
resolves the compatible `webm-vp9-opus-source-v1` artifact for that immutable
selection. The caller cannot choose an artifact path, ambient SourceStream
fallback, codec, profile, or source file.
`preparing/proxy` identifies the existing durable proxy job.
`preparing/integrity` may accompany a succeeded proxy Job while the large media
payload is verified outside API readiness. It does not create a hidden second
job shape.

API cold-start reconciliation validates proxy manifests, exact directory
shape, byte sizes, and small time-map digests, but does not hash the large proxy
media payload. The first delivery verifies its SHA-256 asynchronously and caches
success only while file identity, size, and modification time remain unchanged.

Integrity rejection quarantines the payload, marks the Artifact `evicted`,
retains the succeeded producer Job unchanged, and creates a new durable Job with
`retry_of_job_id`. Only blocked, queued, and running Jobs participate in live
logical-key uniqueness. A successful retry may rematerialize the same semantic
Artifact ID only when producer, parameters, fingerprint, aggregate size, and
content/manifest digest are identical; otherwise it fails closed.

## Same-origin delivery

The installed route is:

```text
Viewer media element
  -> oc://app/api/v1/media/content/<opaque-lease>
  -> Electron main protocol handler
  -> Web sidecar proxy
  -> API media responder
```

Electron injects the renderer-unreadable UI session described in
`specs/ui-session.md`; Web forwards the request. The API validates session,
purpose, expiry, process binding, and optional pinned revisions for every
request.

The responder supports `GET`, `HEAD`, and one RFC-compatible byte range,
including `206`, `Content-Range`, `Accept-Ranges`, exact `Content-Length`, and
strong digest-derived `ETag`. Invalid or multi-range requests fail explicitly.
Response headers prevent content sniffing, cross-origin embedding, caching past
lease expiry, and executable interpretation.

The renderer sees the same-origin opaque URL because browser media elements need
one, but possession outside the bound first-party session is useless. The lease
is not accepted by CLI transport or direct unauthenticated loopback calls.

## Proxy and render inputs

The first release generates immutable seekable preview proxies with explicit
time maps. A Sequence preview lease pins a committed sequence revision and the
RenderPlan/proxy identities used to satisfy it. Proxy substitution preserves
timeline timing, orientation, crop, enabled state, captions, and audio intent;
degradation is reported, not hidden.

A Source preview request pins the expected Asset revision/fingerprint and an
explicit one-video/one-audio-or-less SourceStream selection. Registration's
`default-v1` proxy may satisfy it only when its immutable manifest contains the
same selected IDs; otherwise the API schedules or reuses the corresponding
`explicit-v1` variant. The lease projects only safe exact timing metadata:
Asset identity/revision/fingerprint, selected stream identities, source epoch,
per-track absolute start/finite coverage, and media capability. It exposes no
source path, proxy path, raw manifest or whole VFR time-map bytes.

Bounded source-position resolution is a purpose-bound first-party Viewer read
against the same verified artifact, lease resource ID and UI session. Its
closed operations are `settle | previous | next`; each request repeats the
pinned Asset revision/fingerprint and selected stream IDs, and each response
echoes that closure plus exact source/proxy positions. A selected video stream
settles and steps on presentation boundaries from the verified VFR map; an
audio-only selection settles on source-sample boundaries. The only terminal
synthetic boundary is a proven finite coverage end. The port never returns raw
map bytes or an unbounded position page. It is not a creative write, Agent
command, generic file route or second timeline.

The rendered preview begins at exact Sequence time zero and carries one CFR
video stream and one stereo audio stream. Its lease projection includes the
plan digest, exact semantic duration, frame rate/count, audio sample rate/count,
and declared tail-padding policy required by the playback adapter. These are
metadata, not another editable timeline model.

Thumbnails and waveforms are separate artifact kinds with their own MIME and
bounded response shape. They are never smuggled through arbitrary file-serving
routes.

When no compatible proxy exists, the API schedules or reuses a MediaJob and
returns a typed `preparing` result with job and activity references. It never
substitutes a direct original stream or resolves arbitrary caller-selected
paths.

## Renewal and expiry

The first lease lifetime is five minutes and the value lives only in the API
instance that issued it. Contracts renews through the product port before
expiry when the same viewer purpose, selection, and pinned revisions remain
valid. A renewed lease is a new capability; a URL is never made permanent.

Sequence polling and renewal submit the prior continuation, thereby naming the
same logical job chain, RenderPlan digest once bound, and pinned Sequence
revision. They never resolve the moving Sequence head or choose a different
compatible artifact. Adopting a newer revision discards that continuation only
through an explicit playback-session reconciliation followed by a new
preparation request.

Expiry during playback produces a typed recoverable media state. The Viewer
pauses or continues already buffered bytes according to browser behavior,
renews, verifies that the selected Project/Sequence revision still matches, and
resumes. It never silently switches to a newer creative revision.

Revoking the UI session, replacing any peer process, relinking a changed source,
purging an artifact, or archiving/purging the Project invalidates affected
leases.

## Harness

- seek with `HEAD` and byte ranges through the complete `oc://`/Web/API chain;
- reject expired, copied, cross-session, cross-process, cross-purpose, and direct
  loopback lease use;
- prove every response maps to the pinned digest and never returns a path or
  SourceGrant field;
- renew across ordinary expiry and invalidate across API/Web/Electron instance
  replacement;
- compare proxy time maps with exact source/sequence positions and RenderPlan;
- scan CLI help/results to prove MediaLease is not an Agent-facing capability.
