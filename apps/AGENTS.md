# Application boundaries

- Apps are independently owned runtime roots. Never import another app's
  private `src/` or `sidecar/` files.
- Product types, read/write ports, events, and React hooks belong in
  `packages/contracts`; generated HTTP code belongs in `packages/openapi` and is
  consumed only inside Contracts; shared control-plane mechanics belong in
  `packages/sidecar-client`.
- Each app declares exactly one cell-mode command in `sidecar/manifest.json`.
  Electron and Web point at their sole TypeScript sidecar entry; API points at
  its sole native Go sidecar entry. An app may declare bounded native
  `artifactChecks` in that same manifest so packaging can execute an
  app-owned closure verifier without learning product semantics; checks are not
  runtime entries.
  Keep business source free of sidecar environment, registration, readiness,
  lifecycle, channel, and namespace concepts.
- Sidecar artifacts under `dist/sidecar` are reused unchanged by development,
  packaged payloads, and harnesses.
- Sidecars are lifecycle peers under the shared Go runtime runner. No app sidecar
  may spawn, supervise, or stop another app sidecar; cross-app discovery uses the
  shared sidecar TCP control plane.
- Cross-app dependencies are maintained by reconnecting subscription/reconciliation
  state machines in the consuming sidecar. Never cache a peer endpoint beyond its
  broker generation, state revision, and process instance.
- Tests live in each app's `tests/` directory, not under `src/` or `sidecar/`.
- Web business source is limited to `lib`, `views`, `components`, and `main.tsx`.
  It contains no CSS file, CSS syntax, raw DOM styling, or atom definitions.
  `components` composes atoms from `packages/components`; all product reads,
  writes, and subscriptions use hooks or ports from `packages/contracts`. Web
  source never imports OpenAPI or owns fetch/EventSource transport.
- Creator file picking enters through a Contracts platform port and produces an
  opaque API-owned SourceGrant. Web, Agent, domain entities, and command results
  never own original paths or bookmark/grant material; API-native SourceAccess
  adapters resolve them for pinned media operations.
- Viewer media enters through a Contracts MediaLease and same-origin proxy with
  bounded Range support. Web never constructs artifact/source URLs or receives
  filesystem paths; Electron main injects first-party UI authority without
  exposing it to renderer JavaScript.
- API transport and adapters are split across `controller`, `service`,
  `repository`, and transport-local `model` as needed. Reusable creative domain,
  command schemas, and use cases belong in top-level `product/*`; API layers do
  not recreate them. Sidecar bootstrap stays outside those layers, and OpenAPI
  is derived from the Huma router rather than handwritten schema.
- The API sidecar derives `<launch.dataDir>/api` with shared sidecar mechanics and
  passes that plain directory into API repository startup. The SQLite repository
  derives `database/open-cut.db`, applies embedded ordered migrations, and opens
  successfully before the API publishes an endpoint or READY.
- API business jobs use an internal SQLite lease scheduler and isolated payload-
  pinned processes. They never become another sidecar topology entry, and
  scheduler queue drain is not a READY condition.
- The pinned media manifest and executables are API artifact dependencies below
  `apps/api/dist/sidecar`. API resolves them relative to its physical executable,
  never from datadir, `PATH`, cwd, launch environment, or runtime topology.
  Launcher/runner/sidecar protocol remain unaware of media tool identities.
