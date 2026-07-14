# Cold-start contract

Status: Day 0 baseline.

## Scope

The cold-start substrate launches one user-scoped application cell from an
installer containing only B0. It owns cell identity, roots, a shared loopback TCP
broker, signed release discovery, atomic activation, handoff, readiness, and
rollback. It does not own product data, app HTTP ports, Electron behavior, or
application dependency semantics. Its runtime runner understands only a generic
command topology.

## Identity and roots

A cell is `(channel, namespace)` beneath one OS user. Both values are validated
lowercase safe path segments and are injected, never inferred by children.

```go
type RootSet struct {
    BootstrapRoot string
    StoreRoot     string
    CacheRoot     string
    RuntimeRoot   string
    LogRoot       string
}
```

All roots are clean absolute paths supplied by the embedding/install layer. The
core has no product-aware OS fallback. Mutable roots append:

```text
channels/<channel>/namespaces/<namespace>
```

Logical layout:

```text
<BootstrapRoot>/
  launcher[.exe]
  bootstrap.json

<StoreRoot>/<cell>/
  cell.json
  state/runtime.json
  state/update.json                 # present only during an update transaction
  trust/root.json
  versions/<X.Y.Z-channel.N>/
    manifest.json
    launcher/
    payload/
  incoming/<transaction-id>/tree/

<CacheRoot>/<cell>/downloads/

<RuntimeRoot>/<cell>/
  broker.lock
  control.json
  owner.token

<LogRoot>/<cell>/sessions/<session-id>/
```

Version directories become immutable after same-filesystem promotion.
`state/runtime.json` is atomically replaced and is the sole activation truth.
Cache, runtime, and logs are never correctness inputs.

`state/update.json` is an atomic recovery journal, not activation truth. It
records the verified version, bundle digest, transaction ID, and current phase.
On restart B0 either discards an unpromoted transaction or validates a promoted
tree and completes `state.Prepare`; a crash cannot leave an installed orphan that
silently bypasses the state machine.

## Ownership and lifetime

- B0 owns the broker for the entire cell session and is the only activation-state writer.
- B0 is not a daemon; it exits after the managed launcher/payload tree exits.
- L1 owns steady-state update UX and asks B0 to perform verified transitions.
- A prepared steady-state candidate is handed off inside the same B0 cell session
  after the old runtime tree exits; B0 then applies the normal READY/stability gate.
- A second invocation talks to the existing broker instead of creating another cell.
- The payload exposes one opaque runtime-topology entry. The versioned launcher
  and `oc-control dev` invoke the same generic Go runner against packaged and
  workspace-resolved topology respectively.
- The runner starts peer sidecar processes, aggregates their READY state into the
  broker-visible `payload` session, and independently restarts peers after
  unexpected exits with bounded exponential backoff.
- Before first aggregate READY, the readiness deadline remains strict and failure
  rejects the candidate. After confirmation, peer loss is a recoverable runtime
  condition and does not retroactively change activation state.
- Only explicit broker lifecycle shutdown, B0 cancellation, or loss of the broker
  generation ends the whole runtime tree. Process exit codes do not encode that intent.
- Any binary that accepts a sidecar launch envelope is valid; the runner has no
  Electron, web, API, or product branches.

## Activation state

`runtime.json` contains `schema`, `generation`, `active`, `candidate`,
`lastGood`, and `attempt`. A candidate is confirmed only after authenticated
registration, heartbeats, READY, and a stability hold. Failure rolls back to
`lastGood`; genesis without a last-good version remains in recovery and allows
an explicit retry.

Signed release versions are monotonic within a cell. A valid signature on an
older canonical version does not authorize downgrade or reinstall.

## App sidecar isolation

Each `apps/*/sidecar/manifest.json` is a symmetric, language-neutral sidecar
entry contract. Electron and Web point at independently compiled TypeScript
entries; API points at its native Go artifact. Business code has zero knowledge
of the control plane. Dev, packaged, and harness execution all consume those
same declared artifacts through the generic runtime plan.

Electron is not a sidecar supervisor. It observes the web sidecar's READY state
and published endpoint continuously through the shared cell TCP broker, then
binds that loopback HTTP lease behind the Electron-owned `oc://app/` protocol.
The renderer origin never contains the random sidecar port, and business/UI
source does not know the broker or underlying endpoint. The observer reconciles
revisioned subscription snapshots with periodic TCP polling. Web disappearance
invalidates the protocol target and reverses Electron READY; a new peer instance
or endpoint reloads the same `oc://app/` entry and restores READY. Web and API
remain independently owned peers.
