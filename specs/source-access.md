# Referenced source access

Status: Business design baseline.

## SourceGrant boundary

An Asset never stores or exposes a host path as creative identity. Creator file
selection first creates an opaque installation-local `SourceGrant`; an Asset
registration transaction references its ID.

```text
SourceGrant
  id
  platform
  grantKind and schema
  protected opaque authorization material
  display metadata
  fast file observation
  lifecycle: active | revoked | unavailable
  createdAt / lastResolvedAt?
```

SourceGrant is operational authorization, not Project creative state. Creating a
grant does not advance Project revision. Registering, relinking, changing import
mode, or replacing the grant referenced by an Asset is a creative
EditTransaction.

## Creator selection flow

The creator invokes a source-picker port through `packages/contracts`. In the
desktop product, its platform adapter delegates to Electron main or another
trusted installed picker surface, never renderer JavaScript path handling.

The trusted adapter:

1. asks the OS for creator selection and durable access material;
2. sends that material to an authenticated API SourceGrant registration use
   case through the first-party product session;
3. receives only SourceGrant ID and bounded display metadata;
4. returns those safe fields to Web/Contracts;
5. lets the creator register the grant as a referenced or managed Asset.

The desktop Creator offers both the trusted native picker and drag/drop/file
input. These are two gestures over the same SourceGrant boundary, not two source
access models. For a dropped browser `File`, the context-isolated preload uses
Electron's trusted file identity lookup and passes the resulting absolute path
only to Electron main over one narrow IPC operation. The renderer receives only
an opaque `drop.<token>` authority and sends `{requestId, sourceToken}` to the
same same-origin platform port used by the picker.

Electron main binds each dropped-source token to the originating WebContents,
keeps at most 32 live tokens for at most 60 seconds, and consumes a token exactly
once before SourceGrant registration. Expiry, owner mismatch, malformed token,
untrusted origin, or main-process restart fails closed; none falls back to an
ambient path or silently opens the picker. The absolute path exists only in the
isolated preload call, main's short-lived staging map, and the authenticated
internal SourceGrant registration call. It never enters Contracts results, Web
state, product API routes, activity, logs, or Agent surfaces.

Sandboxed preloads remain a self-contained CommonJS artifact and expose only
`stageDroppedSource(File) -> token`, never `ipcRenderer` or a general host API.
Mac App Store builds reject drop staging because durable access requires a
security-scoped bookmark; their Creator flow retains the trusted native picker.

Web business state, Agent prompt, CLI results, activity, and EditTransaction
payloads never contain the original path or opaque grant bytes.

Browser-only development without a trusted path picker may create a managed copy
through an explicit upload adapter. It cannot pretend to provide durable
referenced access unavailable to that platform.

The desktop adapter uses a same-origin platform port owned by Electron main:
`POST /_open-cut/platform/source-grants`. That route is not an API route and is
handled before renderer proxying. Electron main opens the picker and calls the
authenticated internal API route itself. Both the Electron proxy and the Web
sidecar reject `/api/v1/internal/platform/*`; generated bindings for that route
must never be imported by Contracts or Web.

Closing the picker returns an empty cancellation result and creates neither a
SourceGrant nor an Asset. A successful gesture first commits the SourceGrant,
then performs a distinct creator Asset registration using the current expected
Project revision. A lost or stale second step leaves only an unreferenced grant
eligible for later collection; it never smuggles source authority into creative
state.

## Platform material

The opaque material is adapter-owned. Initial platform forms are:

- macOS security-scoped bookmark and stable file identity;
- Windows normalized access metadata plus file identity and platform protection;
- Linux path grant or desktop-portal document grant, depending on installed
  packaging and sandbox.

The API-owned native SourceAccess adapter can resolve the stored grant without a
live Electron or Web peer. Grant material and any stored path are protected by
platform encryption or strict per-user filesystem permissions below API data and
are excluded from logs, receipts, exports, Agent scratch, and product responses.

Registry, bookmark repair, sandbox renewal, and platform picker details remain
inside SourceAccess/platform adapters. Domain and media pipeline code depend only
on the port.

## SourceAccess port

The product application layer uses an API-native port with operations equivalent
to:

```text
resolve grant -> bounded internal source handle
observe grant -> availability and fast file observation
open grant -> reader/file descriptor for one declared media operation
revoke grant
```

The resolved handle may contain a path, descriptor, or OS handle internally. It
is passed directly to the pinned media subprocess without shell interpolation
and never crosses the product API or CLI boundary.

The first identify executor receives a resolved path only in private stdin. The
path is absent from argv, environment, attempt diagnostics, activity, and
executor output. The executor reopens the source, checks the registered fast
observation before and after a complete SHA-256 read, and returns only the
fingerprint plus the repeated observation.

Each open is scoped to a declared operation and closes deterministically. Losing
access marks the operational Asset state `missing` or `unreadable`; it does not
delete the grant, Asset, clips, transcript, or alignments.

## Identity and relink

SourceGrant proves access, not content identity. After registration, the media
pipeline computes the Asset's complete SHA-256 independently.

Relink creates a new SourceGrant first. The API resolves and hashes the candidate,
then an EditTransaction may atomically replace the Asset's grant only when the
strong identity matches. A mismatch requires explicit creator replacement or a
new Asset and cannot be approved by path similarity, filename, fast observation,
or Agent assertion.

Changing a platform bookmark for the same strong content does not require new
Clip or Alignment identities. The old grant is retained until the transaction
commits, then becomes separately revocable/collectible.

## Managed mode

Managed import reads through SourceAccess into a temporary API-owned file,
computes and verifies the complete SHA-256, atomically publishes bytes, and only
then changes Asset import mode/reference through a creative transaction.

Once managed bytes are authoritative, later loss of the original SourceGrant
does not degrade the Asset. Deleting managed bytes remains a protected action.

## Security invariants

- A SourceGrant ID is not a path and is not sufficient outside an authorized
  product use case.
- No Agent command accepts or returns original path/bookmark/grant material.
- No Agent command accepts or returns dropped-source tokens; those are
  renderer-owner-bound ephemeral authority, not SourceGrant identity.
- No API media operation requires Electron to stay alive.
- No platform picker or grant repair mutates creative state implicitly.
- No source resolution uses shell command construction or ambient working
  directory search.
- No automatic relink scans arbitrary creator directories.
- A revoked grant cannot be reopened by an existing AgentTurn or MediaJob.

## Harness

Platform adapter fixtures prove grant create/resolve/revoke, product restart,
Electron-peer loss, changed/missing/unreadable distinctions, matching and
mismatching relink, managed-copy publication, log redaction, and failure
isolation. The business black box observes only SourceGrant/Asset identities and
typed outcomes.
