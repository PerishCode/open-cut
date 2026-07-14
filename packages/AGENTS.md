# Package boundaries

- Packages are reusable TypeScript libraries and may not import app source.
- `contracts` contains pure product peer identifiers and has no sidecar or HTTP
  schema dependency.
- `components` is the sole atomic component and style owner. Its public props
  never expose `className`, `style`, or raw DOM attribute inheritance.
- `openapi` is the sole product OpenAPI/Orval generation layer. Its committed
  generated code is updated only with an explicitly supplied base URL; Web may
  consume the package but may not recreate request DTOs or clients.
- `sidecar-protocol` contains the pure generated language binding and generic
  zero-dependency decoding runtime for `protocol/sidecar/v1`.
- `sidecar-client` implements transport, reconnection, reconciliation, and
  capability mechanics on top of `sidecar-protocol`; it contains no app identity,
  readiness policy, or business startup logic.
- Tests live in each package's sibling `tests/` directory, never under `src/`.
- Do not create orchestration, packaged, launcher, release, or filesystem policy
  packages here; those mechanisms are owned by Go.
