# Package boundaries

- Packages are reusable TypeScript libraries and may not import app source.
- `contracts` contains product HTTP/event DTOs and has no sidecar dependency.
- `sidecar-protocol` contains the pure generated language binding and generic
  zero-dependency decoding runtime for `protocol/sidecar/v1`.
- `sidecar-client` implements transport, reconnection, reconciliation, and
  capability mechanics on top of `sidecar-protocol`; it contains no app identity,
  readiness policy, or business startup logic.
- Tests live in each package's sibling `tests/` directory, never under `src/`.
- Do not create orchestration, packaged, launcher, release, or filesystem policy
  packages here; those mechanisms are owned by Go.
