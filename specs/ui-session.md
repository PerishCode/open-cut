# First-party UI session

Status: Business design baseline.

## Boundary

The desktop renderer uses a first-party product session that is separate from
the Agent CLI grant, sidecar capabilities, and broker observer authority.
Loopback reachability and the `oc://app/` origin are not authorization.

Lifecycle owns generic installation identity and platform secure-storage
mechanics. The API owns product-session issuance, scope, expiry, revocation, and
audit. Electron main owns session bootstrap and request injection. Renderer
JavaScript never receives a bearer, private key, broker token, or API endpoint.

## Generic installation identity

An installation has a stable opaque installation ID and role-separated signing
keys. The initial roles are:

- `product-cli`, used only for the paired CLI grant;
- `first-party-ui`, used only to prove that Electron main belongs to the current
  installed application.

Lifecycle creates or loads private keys through the platform secure-storage
adapter. A private key is never stored in bootstrap configuration, sidecar
launch data, environment, product SQLite, logs, renderer storage, or Agent
scratch. Reusing one key across roles is forbidden.

The bootstrap layer carries a required versioned, non-secret installation assertion
containing the installation ID, public role keys, and lifecycle generation. B0,
L1, and the generic runner authenticate and forward that assertion opaquely;
they do not interpret Open Cut roles or permissions. Runtime topology cannot
provide, override, or mint it.

The sidecar launch contract carries this assertion as a generic required
`installation` field beside `dataDir`. Its wire shape is authored only in
`protocol/sidecar/v1/main.tsp` and generated normally. Product code
receives only a verified plain startup value from the sidecar entry; business
packages never import the sidecar type.

## Session bootstrap

After Electron main has reconciled current Web and API peer leases, it performs
this bootstrap without renderer participation:

1. Electron main requests a single-use API UI challenge naming the installation
   ID, `first-party-ui` role, cell generation, API process instance, and a fresh
   Electron process instance.
2. The lifecycle secure-storage adapter signs the versioned challenge with the
   UI role key. Electron main never exports that key.
3. The API verifies the public key from its trusted installation startup
   assertion, atomically consumes the nonce, and issues a short-lived UI session
   bound to the installation, cell generation, API instance, Electron instance,
   Web origin, and declared first-party scope.
4. Electron main keeps the session only in main-process memory. It reboots the
   flow after API replacement, broker-generation change, expiry, explicit
   revocation, or Electron restart.

The challenge endpoint is loopback-only, rate-limited, expiry-bound, and proves
possession; it cannot create a Project or mutate product state. A caller cannot
self-select a public key or installation assertion.

## Renderer-unreadable transport

The renderer calls only Contracts ports against its same-origin `/api` surface.
For installed desktop execution:

```text
renderer Contracts
  -> oc://app/api
  -> Electron main protocol handler
  -> Web sidecar API proxy
  -> product API
```

Electron main adds the UI session to the privileged upstream request. The Web
proxy forwards it without exposing it to response headers, page HTML, script,
storage, diagnostics, or Contracts results. The renderer cannot set, inspect,
or override the privileged header. The API rejects a renderer-supplied duplicate
and accepts only the proxy-marked hop from the current peer chain.

The session is ambient authority for the trusted first-party origin, not proof
of creator approval for every effect. Normal product validation and durable
Approval still govern protected actions. Content Security Policy, navigation
isolation, permission policy, and no arbitrary remote content reduce renderer
compromise; the hidden token alone is not claimed to make hostile origin code
safe.

## Browser development

Browser-only development uses an explicitly development-scoped UI signer and
session adapter rooted below the guarded `.tmp/oc-control` cell. The Web sidecar
binds an HttpOnly, SameSite-strict session to its exact development origin. It
uses the same API challenge and session semantics, never an unauthenticated API
mode or production key.

Packaged Web content outside Electron has no first-party session bootstrap in
the first release.

## Scope and lifecycle

The first UI session may:

- read product projections and activity;
- invoke normal creator use cases;
- create and control AgentRuns, Projects, proposals, jobs, and exports;
- approve or deny exact pending Approval records after an explicit creator
  gesture;
- request platform-owned file selection, reveal, relink, and Save As flows
  through Contracts platform ports.

Session expiry does not cancel already durable work. It prevents new calls until
Electron main reauthenticates. UI session audit identifies the installation and
Electron process, while creator actions additionally record the first-party
actor and originating gesture/use case.

## Invariants and harness

- Direct loopback HTTP with no valid UI session or signed CLI request fails.
- CLI signatures cannot be replayed as UI authority, and UI sessions cannot sign
  CLI commands.
- A session from an old API instance, broker generation, Web origin, or Electron
  process is rejected.
- Renderer script, DevTools-visible storage, responses, logs, and crash reports
  contain no session or signing material.
- Refresh and API peer replacement recover by a new main-process bootstrap,
  without weakening to a static token.
- Browser development exercises the same authorization checks and is impossible
  to enable in a packaged release.
