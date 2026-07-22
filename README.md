# open-cut

Open Cut is a pnpm application workspace built on a product-independent Go
cold-start, release, and sidecar-control substrate.

The installed bootstrap surface contains the platform host, launcher, and a
stable product-CLI resolver. A launcher-managed release is one atomic
`launcher + payload` bundle; the opaque payload contains the app runtime and the
versioned product CLI at `payload/bin/open-cut[.exe]`.

Start with [AGENTS.md](./AGENTS.md) and the specifications under [`specs/`](./specs/).

The business baseline is split across
[`product.md`](./specs/product.md),
[`business-kernel.md`](./specs/business-kernel.md),
[`domain-model.md`](./specs/domain-model.md),
[`persistence.md`](./specs/persistence.md),
[`agent-native.md`](./specs/agent-native.md),
[`agent-bridge.md`](./specs/agent-bridge.md),
[`local-authorization.md`](./specs/local-authorization.md),
[`ui-session.md`](./specs/ui-session.md),
[`wire-contract.md`](./specs/wire-contract.md),
[`canonicalization.md`](./specs/canonicalization.md),
[`read-activity.md`](./specs/read-activity.md),
[`media-pipeline.md`](./specs/media-pipeline.md),
[`source-access.md`](./specs/source-access.md),
[`media-delivery.md`](./specs/media-delivery.md),
[`timeline.md`](./specs/timeline.md),
[`transcript-caption.md`](./specs/transcript-caption.md),
[`rendering.md`](./specs/rendering.md),
[`durable-work.md`](./specs/durable-work.md),
[`job-scheduler.md`](./specs/job-scheduler.md),
[`project-lifecycle.md`](./specs/project-lifecycle.md),
[`editing-interaction.md`](./specs/editing-interaction.md),
[`project-versions.md`](./specs/project-versions.md),
[`playback.md`](./specs/playback.md),
[`workspace-ui.md`](./specs/workspace-ui.md) and
[`business-harness.md`](./specs/business-harness.md). These sit above the
product-independent cold-start, sidecar, release, and delivery contracts.

## Day 0 development path

Install the Go version declared by `go.mod` and a Node version satisfying the
root `package.json` first. The repository does not install either runtime.

```sh
go install ./cmd/oc-control
oc-control bootstrap
oc-control doctor
oc-control protocol check
oc-control clean --scope quick
oc-control dev
```

`oc-control bootstrap` validates the installed Node and pnpm versions, performs
a frozen workspace install, and enables the repository pre-commit hook. It never
installs or replaces development tools. After control source changes, rerun
`go install ./cmd/oc-control`.

By default `oc-control dev` owns
`<repo>/.tmp/oc-control/dev/dev/default`; the final two segments are the cell's
channel and namespace. `--base-dir` may select another clean absolute path with
the same suffix. The runner passes that final path unchanged, each sidecar
derives its app directory, and the API stores SQLite at
`<base-dir>/api/database/open-cut.db`.

The current executable acceptance paths are:

```sh
oc-control harness broker
oc-control harness sidecars
oc-control harness cold-start
oc-control pack mac --arch arm64 --version 0.1.0-beta.1
oc-control verify mac --arch arm64 --bundle dist/releases/0.1.0-beta.1/mac-arm64/open-cut-0.1.0-beta.1-mac-arm64.release-bundle.tar.zst
```

- `broker` exercises the real cell lock, TCP rendezvous, capabilities, WebSocket,
  endpoint publication, READY, and authenticated status.
- `sidecars` builds and executes the unique web/API sidecar entries against the
  Go broker, verifies that API migrations and SQLite initialization finish before
  READY, then verifies shared shutdown and clean process exit.
- `cold-start` builds real B0/L1 and fixture payload binaries, performs genesis
  confirmation, rotates the trust root, executes a broker-mediated v1→v2
  steady-state handoff, proves offline last-good boot, and proves pre-READY rollback.
- `dev` builds and starts the generic Go runtime runner in headed mode. Electron,
  web, and API are peer sidecars; Electron discovers the web endpoint through the
  shared TCP broker and never owns the other processes. Its renderer always loads
  `oc://app/`; an Electron protocol adapter proxies that stable origin to the current
  loopback Web lease. Web runs React through Vite in dev and serves the Vite
  production build through the same thin sidecar wrapper in a release. Its
  stable `/api` ingress continuously follows the native Go API sidecar lease.
- Sidecar state is continuously reconciled over TCP. Revisioned WebSocket snapshots
  provide low-latency changes, status polling repairs gaps, and the runner restarts
  unexpectedly exited peers without changing ownership boundaries.
- The sidecar wire contract is authored only in `protocol/sidecar/v1/main.tsp`.
  `oc-control protocol generate` produces OpenAPI, JSON Schema, and the Go/TypeScript
  bindings and decoders in `packages/sidecar-protocol`; transport and reconciliation
  stay in `packages/sidecar-client`. `oc-control protocol check` rejects generated drift.
- `pack` discovers every app sidecar from its language-neutral manifest, deploys their
  production trees, generates a platform-resolved generic runtime topology,
  builds the Electron full pack and `cmd/cli`, and archives them with the
  versioned launcher.
- `full-pack` extracts that real archive and invokes the versioned L1 launcher,
  proving that the same runner starts independent Electron/web/API peers,
  aggregates READY, publishes endpoints, broadcasts lifecycle control, and exits
  the runtime tree cleanly without a GUI.

### UI development loop

Keep exploratory UI work out of the default dev data by giving the cell an
isolated base directory. The path must end in the canonical `dev/default` cell
suffix, and every companion command must use the same value:

```sh
oc-control dev --base-dir .tmp/oc-control/ui-audit/dev/default
```

From another terminal, inspect the live Electron renderer without discovering
its transient CDP port by hand:

```sh
oc-control dev inspect --base-dir .tmp/oc-control/ui-audit/dev/default \
  --screenshot .tmp/ui-audit.png
oc-control dev inspect --base-dir .tmp/oc-control/ui-audit/dev/default \
  --eval 'document.body?.innerText'
oc-control dev inspect --base-dir .tmp/oc-control/ui-audit/dev/default \
  --set-file .tmp/fixture.webm
```

`--set-file` accepts only a non-empty regular file, reports the exact attached
byte size, and targets the first enabled file input. It is a generic renderer
actuator; it does not import media through an internal API or learn product
semantics.

When a reproducible local media fixture is needed, record the running renderer
through the contained media toolchain. On macOS, optional narration gives the
transcription path known words without adding a checked-in binary fixture:

```sh
oc-control dev record --base-dir .tmp/oc-control/ui-audit/dev/default \
  --output .tmp/ui-audit.webm --duration 8 \
  --speech 'A clear local workflow keeps creative work moving.'
```

Use `Ctrl-C` to end the owning `dev` command. `oc-control clean --scope quick`
removes stopped ad-hoc dev and harness data while preserving the expensive
media-toolchain cache; it refuses directories belonging to a live cell.

## Local delivery loop

Every public target is named `<platform>-<arch>`, using only `mac|win|linux` and
`arm64|x64`. Go/Electron internal names such as `darwin` and `amd64` do not appear
in release paths or artifact names.

The macOS Day 0 loop is:

```sh
version=0.1.0-beta.1
target=mac-arm64
bundle=".tmp/delivery/open-cut-${version}-${target}.release-bundle.tar.zst"

oc-control release keygen --output .tmp/delivery/key.json --id local
oc-control pack mac --arch arm64 --version "$version" --output "$bundle"
oc-control release create --bundle "$bundle" --origin .tmp/delivery/origin --key .tmp/delivery/key.json
oc-control verify mac --arch arm64 --origin .tmp/delivery/origin --channel beta --key .tmp/delivery/key.json
oc-control serve --root .tmp/delivery/origin --listen 127.0.0.1:41000
```

With that origin running, a separate terminal can simulate final-user actions:

```sh
workspace=.tmp/delivery/install-case
receipt="$workspace/receipts/install-receipt.json"

oc-control harness install mac --arch arm64 --workspace "$workspace" \
  --origin .tmp/delivery/origin --origin-url http://127.0.0.1:41000 \
  --key .tmp/delivery/key.json --headless
oc-control inspect --receipt "$receipt"
"$(jq -r .cliPath "$receipt")" status --receipt "$receipt"
oc-control harness run --workspace "$workspace" --receipt "$receipt" --headless
oc-control harness uninstall --workspace "$workspace" --receipt "$receipt" --purge
```

The receipt lives outside the installed application so uninstall is repeatable.
`harness run` executes the installed platform host, not a source-tree shortcut.
The installed CLI resolver reads `runtime.json.active`, dispatches to the fixed
CLI path in that version, and uses the broker's observe-only token only for
control-plane discovery. A non-help business command asks the installed
lifecycle host to ensure readiness, then the versioned CLI reaches the product
API with a separate absorbed business grant; it never joins runtime topology or
inherits lifecycle authority from the API.
The public CI builds and verifies native `mac-arm64`, `win-x64`, and `linux-x64`
full packs; macOS additionally runs the install/offline-relaunch/uninstall loop.

### Delivery timing reports

Use a structured timing report when comparing delivery changes instead of
inferring performance from the duration of one compound shell step:

```sh
oc-control pack mac --arch arm64 --version 0.1.0-beta.1 \
  --output "$bundle" \
  --timing-report .tmp/oc-control/timing/mac-arm64/pack.json

oc-control timing summary \
  --report .tmp/oc-control/timing/mac-arm64/media-toolchain.json \
  --report .tmp/oc-control/timing/mac-arm64/whisper-toolchain.json \
  --report .tmp/oc-control/timing/mac-arm64/pack.json

oc-control timing compare \
  --baseline .tmp/timing-baseline/pack.json \
  --candidate .tmp/oc-control/timing/mac-arm64/pack.json
```

The API sidecar build automatically writes media and transcription reports
under `.tmp/oc-control/timing/<target>/`. Harness commands write both their
normal report and `reports/timing.json` below the selected workspace. Reports
are written on failure as well as success and record reuse decisions separately
from phase duration.

Each native CI lane publishes these JSON reports as artifacts and renders them
in the job summary. Its cache report distinguishes an exact key hit, a
restore-prefix fallback, and a miss; the media reports independently show
whether the restored closure and compiled C tree were actually reused. Compare
the same target and cache cohort before attributing a duration change to code.

Generated workspace cleanup is deliberately repository-scoped:

```sh
oc-control clean --scope quick      # per-entry .tmp cleanup; keeps media-toolchain caches
oc-control clean --scope temp       # .tmp only
oc-control clean --scope build      # apps/*/dist and packages/*/dist
oc-control clean --scope all        # both generated surfaces
oc-control clean --scope all --dry-run
```

Every report item carries its measured byte size, so `--dry-run` doubles as a
disk usage probe over the generated surfaces. `quick` removes harness
workspaces, ad-hoc debug directories, and the dev cell, but keeps the
`media-toolchain` download cache whose loss forces a full native recompile.
Directories holding a held `broker.lock` belong to a running cell and are
reported `in-use` instead of being removed, in every scope.

The command never accepts arbitrary deletion targets and never removes dependency
trees. Use it instead of shell-recursive deletion during normal development.

Run repository checks with:

```sh
go test ./...
pnpm build
pnpm format
pnpm lint
pnpm test
oc-control protocol check
oc-control harness guard
```
