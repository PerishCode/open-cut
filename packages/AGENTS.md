# Package boundaries

- Packages are reusable TypeScript libraries and may not import app source.
- `contracts` is the sole Web-facing product communication boundary. It owns
  stable read/write types and ports, runtime DTO validation, EventBus/SSE
  reconciliation, and React Provider/hooks. It may consume generated operations
  from `openapi`, but it never exposes Orval DTOs as its public contract.
  Sidecar-only peer identifiers use the narrow `contracts/runtime-peer` subpath
  so native sidecars do not load the React/OpenAPI application graph.
- `components` is the sole atomic component and style owner. Its public props
  never expose `className`, `style`, or raw DOM attribute inheritance.
- `openapi` is the sole product OpenAPI/Orval generation layer. Its package root
  points directly at the single generated entry; do not add handwritten exports.
  Only Contracts consumes it from application code.
- `sidecar-protocol` contains the pure generated language binding and generic
  zero-dependency decoding runtime for `protocol/sidecar/v1`.
- `sidecar-client` implements transport, reconnection, reconciliation, and
  capability mechanics on top of `sidecar-protocol`; it contains no app identity,
  readiness policy, or business startup logic.
- Tests live in each package's sibling `tests/` directory, never under `src/`.
- Do not create orchestration, packaged, launcher, release, or filesystem policy
  packages here; those mechanisms are owned by Go.
