# Package boundaries

- Packages are reusable TypeScript libraries and may not import app source.
- `contracts` contains product HTTP/event DTOs and has no sidecar dependency.
- `sidecar-client` implements the language-neutral protocol under
  `protocol/sidecar/v1`; it contains no app identity, readiness policy, or
  business startup logic.
- Do not create orchestration, packaged, launcher, release, or filesystem policy
  packages here; those mechanisms are owned by Go.
