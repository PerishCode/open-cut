# Application boundaries

- Apps are independently owned runtime roots. Never import another app's
  private `src/` or `sidecar/` files.
- Product types, read/write ports, events, and React hooks belong in
  `packages/contracts`; generated HTTP code belongs in `packages/openapi` and is
  consumed only inside Contracts; shared control-plane mechanics belong in
  `packages/sidecar-client`.
- Each app declares exactly one cell-mode command in `sidecar/manifest.json`.
  Electron and Web point at their sole TypeScript sidecar entry; API points at
  its sole native Go sidecar entry.
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
- API resources are split across `controller`, `service`, `repository`, and
  `model`. Sidecar bootstrap stays outside those layers, and OpenAPI is derived
  from the Huma router rather than handwritten schema.
- The API sidecar derives `<launch.dataDir>/api` with shared sidecar mechanics and
  passes that plain directory into API repository startup. The SQLite repository
  derives `database/open-cut.db`, applies embedded ordered migrations, and opens
  successfully before the API publishes an endpoint or READY.
