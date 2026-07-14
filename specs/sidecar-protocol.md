# Sidecar control protocol

Status: v1 draft implemented from the language-neutral artifacts under
`protocol/sidecar/v1`.

Each cell has one B0-owned listener on an ephemeral `127.0.0.1` TCP port.
`control.json` publishes the endpoint, PID, session ID, generation, and protocol
version without secrets. HTTP/JSON handles requests; WebSocket handles registered
sidecar sessions and events.

B0 mints HMAC-authenticated, session-bound, expiring capability tokens. A token
names one subject, role, generation, and capability set. One shared cell bearer
token is forbidden. The permission-restricted `owner.token` is itself an admin
capability, not a signing secret.

Initial capabilities:

- `observe`: health, status, and session inspection;
- `runtime-ready`: register, heartbeat, publish endpoint, and READY;
- `lifecycle`: receive show and shutdown commands;
- `update-transition`: managed-runtime request for B0 to prepare the latest release;
- `delegate-sidecar`: runtime-role only; request one short-lived, subject-bound
  child token whose lifetime cannot exceed the parent runtime token.

Active runtime and sidecar sessions renew their own capability lease through
`POST /v1/capabilities/renew`. Renewal copies the authenticated subject, role,
delegation ancestry, and exact capability set; it cannot add authority. The Go
runner and TypeScript sidecar client renew before expiry, while B0 rotates the
owner capability file atomically for the lifetime of the broker generation.

Sidecars send a registration message first, then heartbeat, endpoint, reversible
READY state, and exit messages. The broker rejects mismatched channel, namespace,
session, generation, subject, process instance, or capability. A session token
becomes invalid when B0 exits or the generation changes.

Every material state mutation increments a cell-local `revision`: registration,
endpoint replacement, READY transition, and session removal. The registered
WebSocket carries full revisioned status snapshots alongside lifecycle commands.
Clients also poll `GET /v1/status` at a lower frequency and accept the newest
snapshot, closing subscription races and repairing missed frames.

The client transport is reconnecting. It retains only its own desired endpoints
and READY state, re-registers with the same process `instanceId`, and replays that
state after reconnect. Observed peer endpoints are never retained as authority
when the observer loses synchronization. A restarted process has a new
`instanceId`; its endpoint is a new lease even when the URL happens to be equal.

READY is reversible for app sessions. Transitioning to not-ready clears published
endpoints. The aggregate `payload` READY remains a one-time candidate confirmation
gate: later peer degradation drives recovery but does not undo release activation.

Sidecar-role tokens are app-subject-bound and may observe peer READY/endpoints
within the same authenticated cell. The shared Go runtime runner receives the
runtime-role token; its registered app label remains generic while the token
subject remains the broker-visible `payload` session used for confirmation.
The runner may request distinct child tokens from
`POST /v1/capabilities/sidecar`; delegated tokens contain `delegatedBy=payload`,
cannot delegate again, and never receive update capabilities.

The runner consumes generated topology and performs one delegation request per
peer subject. It passes only that peer's launch envelope to the process. No
cell-wide token is placed in the payload tree or shared between Electron, web,
API, and the aggregate runtime session. App sidecars never spawn or own peers.
Base delegated capabilities are READY/lifecycle/observe. A topology process may
explicitly request `update-transition`; it is never granted implicitly, and no
other capability escalation is accepted.

Within a live broker generation, unexpected peer exits are restarted with bounded
exponential backoff. An explicit broker `shutdown` command is the only app-visible
request to end the entire cell tree. Loss of B0/broker ends the generation; a new
launcher invocation creates a new authority rather than resurrecting the old one.

## Steady-state transition

`POST /v1/update/transition` currently accepts only `prepare-latest`. The
runtime-role capability or an explicitly topology-authorized app capability may
call it; ordinary app tokens and the owner control token may not. The broker
serializes requests and invokes a B0-owned handler.
B0 alone fetches root/release metadata, verifies, downloads, promotes, journals,
and prepares activation state. A `prepared` response includes the canonical
version and `restartRequired=true`; lifecycle shutdown then lets B0 hand off the
candidate. Confirmation and rollback are never caller-selected HTTP actions.
