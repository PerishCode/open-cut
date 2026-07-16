# Repository guide

This repository is a polyglot pnpm and Go workspace. Read this file before
changing repository-wide lifecycle, release, launcher, sidecar, or app-entry
behavior. Add narrower `AGENTS.md` files only when a directory gains enough
local policy to justify one.

## Architecture boundaries

- `apps/web`, `apps/api`, and `apps/electron` are product application roots.
- `product/*` is the reusable Go business kernel: domain, command/schema
  registry, and application ports/use cases. It imports no app-private source or
  cold-start/control-plane package.
- `packages/*` contains reusable TypeScript packages. Apps never import another
  app's private source.
- `cmd/launcher` and Go packages under `internal/` own bootstrap, update,
  handoff, activation state, and the cell TCP broker.
- `cmd/oc-control` is the only development and operations control CLI. Do not
  recreate `tools/*`, `tools-*`, or `apps/packaged` orchestration layers.
- `cmd/cli` is the independently versioned product CLI. It is packaged at the
  fixed payload path `payload/bin/open-cut[.exe]`; an installed stable resolver
  dispatches to `runtime.json.active` and attaches to the cell with the
  observe-only token. Its control-plane attachment remains observe-only; product
  business commands use a separate product port and grant behind the CLI. It
  never imports `apps/api` private source or joins runtime topology.
- The launcher and `oc-control dev` consume the same generic runtime-topology
  contract. They execute declared commands but do not model Electron, web, API,
  product identity, application dependencies, or business startup semantics.
- `apps/api` is a product API. It does not host or proxy the sidecar control plane.
- The API sidecar is the sole owner of the product SQLite database. It receives
  its app data directory from the generic sidecar launch envelope; API repository
  code alone derives database and migration paths below that directory.
- B0 is the only writer of activation state. Tools and children request live-cell
  transitions through the loopback TCP broker.

## Product communication boundary

- `packages/openapi` is a completely generated package whose root export points
  directly at the Orval output. Do not add handwritten re-export files.
- `packages/contracts` owns stable product read/write ports, runtime validation,
  EventBus reconciliation, SSE transport, and React Provider/hooks. Generated
  OpenAPI operations are an internal adapter, not the Web-facing contract.
- `apps/web/src` imports product communication only from `packages/contracts`.
  It must not import `packages/openapi` or own `fetch`/`EventSource` transport.
- `apps/api` owns product OpenAPI endpoints and the SSE stream. Transport can be
  optimized inside a layer, but Web → Contracts → transport/proxy → API remains
  the fixed logical chain.
- Go Agent command schemas originate only in `product/command`; Huma derives the
  product HTTP OpenAPI from the same DTOs, and CLI JSON help comes from the same
  registry. Do not add handwritten command JSON Schema or a parallel product
  TypeSpec.

## Agent-native business boundary

- The installed stable `open-cut` CLI is the sole Agent-facing product entry.
  The Agent discovers and executes behavior only through
  `<cli> <command> <subcommand> [--help]` from the shipped prompt.
- Do not add an Agent-facing MCP server, SDK, HTTP or socket endpoint, database
  contract, project-file contract, sidecar capability, or second Open Cut Agent
  entry executable.
  Internal transports remain hidden CLI implementation details.
- Narrative and Sequence are peer creative truths connected by durable explicit
  Alignment entities. All creative mutation is a revisioned, idempotent,
  atomic EditTransaction; no implicit cross-model synchronization is allowed.
- Durable entity IDs are service-generated UUIDv7 values. Proposal/request and
  RenderPlan digests use fully normalized domain-separated canonical JSON; no
  client-selected durable ID, hidden default, or automatic transaction splitting
  is allowed.
- Agent black-box acceptance receives only the shipped prompt and stable CLI
  path. Delivery setup may use `oc-control`, but `oc-control`, launcher, runner,
  and broker never acquire product or creative semantics.
- CLI business authorization uses an installation key in lifecycle-owned secure
  storage plus API-owned pairing and request verification. Never expose key,
  challenge, signature, broker token, or UI session material through AppState,
  Agent environment, help, or command results.
- First-party UI authorization uses a different lifecycle-owned role key.
  Electron main alone bootstraps and holds the short-lived API session, then
  injects it through the `oc://`/Web proxy chain; renderer JavaScript never owns
  it. Direct loopback access remains unauthorized.
- The API Agent bridge may launch a configured existing local Agent with a
  bounded prompt, per-turn scratch directory, stable CLI on `PATH`, and safe
  AppState context environment. It never gives the Agent an internal endpoint,
  token, data path, sidecar capability, or private operation schema.

## Sidecar entry contract

- `protocol/sidecar/v1/main.tsp` is the sole sidecar wire-contract source.
  `oc-control protocol generate` owns OpenAPI, JSON Schema, Go, and TypeScript
  artifacts; never edit generated protocol files directly. CI uses
  `oc-control protocol check` to reject drift.
- Generated TypeScript protocol types, constants, schemas, and decoders live in
  `packages/sidecar-protocol`; transport and reconciliation mechanics live in
  `packages/sidecar-client`.
- Each `apps/*/sidecar/manifest.json` is the sole language-neutral declaration
  of that app's runtime command and artifact closure. `$node` and `$payload` are
  generic runner tokens; any other command is an app-relative native artifact.
  Optional app-relative native `artifactChecks` are packaging gates only:
  `oc-control` executes them but never interprets their product semantics or
  emits them into runtime topology.
- Web and Electron compile their sole sidecar-mode source to
  `dist/sidecar/index.js`; API builds its Go sidecar to
  `dist/sidecar/api-sidecar.exe`. Development, packaged execution, and harnesses
  consume those same outputs; do not add mode-specific entries.
- App sidecars are peer processes. Electron must discover web state/endpoints
  through sidecar IPC and must never spawn, supervise, or stop web/API itself.
- Cross-sidecar state is a continuous TCP subscription plus reconciliation
  relationship, never a one-shot startup query. Peer sessions and endpoints are
  leases tied to broker generation, state revision, and process instance.
- Performance work may optimize transport and state propagation inside a layer,
  but must never bypass or collapse launcher, runner, broker/client, app sidecar,
  and business boundaries.
- Business source must not import sidecar, channel, namespace, broker, capability,
  heartbeat, READY, or packaged-mode concepts.
- Sidecar entries may import normal app startup primitives and shared
  `packages/sidecar-client` mechanics. The dependency direction never reverses.
- `SidecarLaunch.dataDir` is a required clean absolute, cell-scoped base path.
  The runner forwards it outside runtime topology; each sidecar appends only its
  validated app identity. Topology and business source never select or override it.

## Persistent data path

- Installer/lifecycle adapters resolve packaged data as
  `<OS-user-data-root>/<stable-product-id>/<channel>/<namespace>` and persist the
  final clean absolute path in bootstrap configuration. Platform defaults,
  Windows custom paths, registry state, repair, and uninstall are absorbed there.
- `oc-control dev` defaults to
  `<repo>/.tmp/oc-control/dev/<channel>/<namespace>`. Other runtime harnesses use
  their canonical subcommand directory under `.tmp/oc-control` and the same
  direct cell suffix. Do not restore `channels/` or `namespaces/` label layers.
- Paths never include release version, architecture, PID, session, mode, or
  process instance. Runtime topology, sidecars, and API code never infer an OS path.
- SQLite migrations are an immutable, strictly ordered forward sequence. There
  is no down migration or old-binary availability guarantee after schema advance.
- Product persistence uses an append-only proposal/transaction journal,
  normalized current projections, and a transactional activity outbox. A
  creative commit updates all three atomically; normal startup does not rebuild
  state by replaying history.
- Product JSON wires encode RationalTime numerators, revisions, and activity
  cursors as canonical decimal strings. Web/Contracts must not coerce them to
  unsafe JavaScript numbers.
- Media, resource, and export work runs through an API-internal SQLite lease
  scheduler. Do not add a product worker sidecar or make API READY wait for the
  business queue to drain.

## Hot development and operations path

Go (at the version declared by `go.mod`), Node `~24`, and the exact pnpm version
pinned by the root `packageManager` are development prerequisites. The repository
never installs or replaces these tools.
After a fresh checkout, install the current control CLI and initialize the
development surface:

```sh
go install ./cmd/oc-control
oc-control bootstrap
```

`oc-control bootstrap` validates Node and pnpm, runs
`pnpm install --frozen-lockfile`, and configures `core.hooksPath` for the
repository pre-commit gate. It never installs or replaces development tools.
Rerun it after root toolchain or lockfile changes; rerun `go install
./cmd/oc-control` after control source changes.

Then use the installed binary for lifecycle, packaging, fixtures, releases, and
harnesses:

```sh
oc-control <subcommand>
```

After changing the sidecar wire contract, run `oc-control protocol generate`.

Do not add local wrappers around `oc-control`, Node, or pnpm, use `go run` as a
documented hot path, or mutate `runtime.json` from scripts. CI and other isolated
workflows may `go build` a temporary control binary and execute that path directly.

Use `oc-control clean --scope temp|build|all` for generated workspace cleanup.
Do not use shell-recursive deletion as the normal cleanup path. The command is
repository-guarded and deliberately excludes source, dependencies, and arbitrary paths.

## Workspace workflow

- Node is `~24`; pnpm is pinned by root `package.json` and invoked directly.
- Root `package.json` exposes only `build`, `format`, `lint`, and `test`; each is
  `pnpm -r --if-present run <name>`. Use package-scoped pnpm scripts for narrower checks.
- Biome is the sole formatter, linter, and import organizer for handwritten
  TypeScript across `apps/*` and `packages/*`. Its version comes only from the
  workspace catalog. Generated protocol bindings remain generator-owned.
- Go formatting and tests are repository-wide: `gofmt` and `go test ./...`.
- `oc-control harness guard` enforces app/package directory boundaries, Web
  Contracts-only communication and zero-CSS policy, shared atom ownership, API layer imports, sibling test
  directories, the 50 KiB resource limit, and the 800-line file limit.
- Pre-commit rejects partially staged files, runs gofmt and recursive Biome
  formatting transactionally, then requires guard and recursive lint to pass.
- `oc-control pack` may invoke pinned pnpm build scripts, but archive creation,
  verification, and extraction stay implemented in Go.
- Public targets are always `<mac|win|linux>-<arm64|x64>`. Keep Go and Electron
  target spellings behind `utils/target`; never leak `darwin` or `amd64` into
  artifact names, release metadata paths, or user-facing commands.
- Final-user delivery checks use `oc-control harness install|run|uninstall` and
  the external install receipt. Do not bypass the installed platform host in CI.
- Generated output and local runtime roots stay out of Git.

## Long-running task memory

Use `.task/` only for multi-round work. Keep active execution truth in
`.task/MAIN.md`, settled history in numbered `.task/phases/`, and support
material in `.task/resources/`. Keep `.task/` ignored by default. Ask before
deleting it when a task completes.
