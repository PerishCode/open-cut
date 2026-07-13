# Repository guide

This repository is a polyglot pnpm and Go workspace. Read this file before
changing repository-wide lifecycle, release, launcher, sidecar, or app-entry
behavior. Add narrower `AGENTS.md` files only when a directory gains enough
local policy to justify one.

## Architecture boundaries

- `apps/web`, `apps/api`, and `apps/electron` are product application roots.
- `packages/*` contains reusable TypeScript packages. Apps never import another
  app's private source.
- `cmd/launcher` and Go packages under `internal/` own bootstrap, update,
  handoff, activation state, and the cell TCP broker.
- `cmd/oc-control` is the only development and operations control CLI. Do not
  recreate `tools/*`, `tools-*`, or `apps/packaged` orchestration layers.
- The launcher treats the built Electron tree as one opaque payload. It does not
  model Electron, web, API, product identity, or the payload's process graph.
- `apps/api` is a product API. It does not host or proxy the sidecar control plane.
- B0 is the only writer of activation state. Tools and children request live-cell
  transitions through the loopback TCP broker.

## Sidecar entry contract

- `apps/web/sidecar/index.ts` and `apps/api/sidecar/index.ts` are the sole
  sidecar-mode source entries for those apps.
- Both compile to `dist/sidecar/index.js`. Development, Electron packaging, and
  harnesses consume that same output; do not add dev- or packaged-only entries.
- Business source must not import sidecar, channel, namespace, broker, capability,
  heartbeat, READY, or packaged-mode concepts.
- Sidecar entries may import normal app startup primitives and shared
  `packages/sidecar-client` mechanics. The dependency direction never reverses.

## Hot development and operations path

After a fresh checkout or a change under `cmd/`, `internal/`, `sidecar/`, or
`protocol/`, install the current control CLI:

```sh
go install ./cmd/oc-control
```

Then use the installed binary for lifecycle, packaging, fixtures, releases, and
harnesses:

```sh
oc-control <subcommand>
```

Do not add pnpm wrappers around `oc-control`, use `go run` as a documented hot
path, or mutate `runtime.json` from scripts. CI may build a checkout-pinned
binary instead of relying on a machine-global installation.

Use `oc-control clean --scope temp|build|all` for generated workspace cleanup.
Do not use shell-recursive deletion as the normal cleanup path. The command is
repository-guarded and deliberately excludes source, dependencies, and arbitrary paths.

## Workspace workflow

- Node is `~24`; pnpm is pinned by root `package.json`.
- Use package-scoped pnpm scripts for app and package checks.
- Go formatting and tests are repository-wide: `gofmt` and `go test ./...`.
- `oc-control pack` may invoke pinned pnpm build scripts, but archive creation,
  verification, and extraction stay implemented in Go.
- Public targets are always `<mac|win|linux>-<arm64|x64>`. Keep Go and Electron
  target spellings behind `internal/target`; never leak `darwin` or `amd64` into
  artifact names, release metadata paths, or user-facing commands.
- Final-user delivery checks use `oc-control harness install|run|uninstall` and
  the external install receipt. Do not bypass the installed platform host in CI.
- Generated output and local runtime roots stay out of Git.

## Long-running task memory

Use `.task/` only for multi-round work. Keep active execution truth in
`.task/MAIN.md`, settled history in numbered `.task/phases/`, and support
material in `.task/resources/`. Keep `.task/` ignored by default. Ask before
deleting it when a task completes.
