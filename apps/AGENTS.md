# Application boundaries

- Apps are independently owned runtime roots. Never import another app's
  private `src/` or `sidecar/` files.
- Shared business contracts belong in `packages/contracts`; shared control-plane
  mechanics belong in `packages/sidecar-client`.
- `web/sidecar/index.ts` and `api/sidecar/index.ts` are the only cell-mode entries.
  Keep business source free of sidecar environment, registration, readiness,
  lifecycle, channel, and namespace concepts.
- Sidecar entries compile to `dist/sidecar/index.js` and are reused unchanged by
  development, packaged payloads, and harnesses.
- Tests live in each app's `tests/` directory, not under `src/` or `sidecar/`.
