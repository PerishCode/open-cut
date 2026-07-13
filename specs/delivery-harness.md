# Delivery harness contract

Status: Day 0 baseline.

The delivery harness exercises the same boundaries a final user crosses. It may
choose isolated filesystem roots, development trust keys, and a loopback release
origin, but it cannot start a payload or sidecar directly.

## Commands

```text
oc-control harness install <platform> --arch <arch> ...
oc-control inspect --receipt <path>
oc-control harness run --workspace <path> --receipt <path>
oc-control harness uninstall --workspace <path> --receipt <path> [--purge]
```

`install` verifies the target origin, creates the platform launcher installation,
writes injected absolute roots and initial trust, then starts the installed
platform host. `run` starts that same installed host again. Both return only
after the opaque payload and its required sidecars report READY through the cell
TCP broker. `inspect` reads live broker state using the receipt-derived cell.

The authoritative harness receipt is outside the installed application. A copy
inside the platform application is consumed by the platform host, but uninstall
never depends on that copy surviving. Every removable path must be a clean
absolute path contained by the declared harness workspace.

`uninstall` requests broker-mediated shutdown before removing the platform
installation. `--purge` additionally removes every receipt-owned cold-start root.
The operation is convergent and repeatable: missing installation or root paths
remain a successful end state, and the external receipt remains available for a
second invocation.

Native CI builds and verifies a full pack on each supported runner. The macOS
adapter additionally proves first-network install, live inspection, clean
shutdown, offline last-good relaunch, and idempotent purge. Windows and Linux
platform installer adapters can extend the same receipt contract without adding
runtime knowledge to launcher.
