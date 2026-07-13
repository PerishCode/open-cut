# open-cut

Open Cut is a pnpm application workspace built on a product-independent Go
cold-start, release, and sidecar-control substrate.

The installer contains only the bootstrap launcher. A launcher-managed release
is one atomic `launcher + payload` bundle, where the payload is currently a full
Electron pack but remains opaque to launcher code.

Start with [AGENTS.md](./AGENTS.md) and the specifications under [`specs/`](./specs/).

## Day 0 development path

```sh
pnpm install
go install ./cmd/oc-control
oc-control doctor
oc-control clean --scope temp
```

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
  Go broker, then verifies shared shutdown and clean process exit.
- `cold-start` builds real B0/L1 and fixture payload binaries, performs genesis
  confirmation, rotates the trust root, executes a broker-mediated v1→v2
  steady-state handoff, proves offline last-good boot, and proves pre-READY rollback.
- `pack` discovers app sidecars from their unique source entries, deploys their
  production trees, generates the payload topology, builds the platform Electron
  full pack, and archives it with a versioned launcher.
- `full-pack` extracts that real archive and runs its Electron binary as the Node
  carrier, proving restricted child delegation, packaged web/API READY, endpoint
  publication, shared lifecycle control, and clean runtime-tree exit without a GUI.

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
oc-control harness run --workspace "$workspace" --receipt "$receipt" --headless
oc-control harness uninstall --workspace "$workspace" --receipt "$receipt" --purge
```

The receipt lives outside the installed application so uninstall is repeatable.
`harness run` executes the installed platform host, not a source-tree shortcut.
The public CI builds and verifies native `mac-arm64`, `win-x64`, and `linux-x64`
full packs; macOS additionally runs the install/offline-relaunch/uninstall loop.

Generated workspace cleanup is deliberately repository-scoped:

```sh
oc-control clean --scope temp       # .tmp only
oc-control clean --scope build      # apps/*/dist and packages/*/dist
oc-control clean --scope all        # both generated surfaces
oc-control clean --scope all --dry-run
```

The command never accepts arbitrary deletion targets and never removes dependency
trees. Use it instead of shell-recursive deletion during normal development.

Run repository checks with:

```sh
go test ./...
pnpm typecheck
```
