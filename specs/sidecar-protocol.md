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

Sidecars send a registration message first, then heartbeat, endpoint, READY, and
exit messages. The broker rejects mismatched channel, namespace, session,
generation, subject, or capability. A session token becomes invalid when B0 exits
or the generation changes.

Sidecar-role tokens are app-subject-bound. The one opaque root receives a
runtime-role token: its registered app label remains opaque to B0, while the
token subject remains the broker-visible `payload` session used for confirmation.
The runtime may request distinct child tokens from
`POST /v1/capabilities/sidecar`; delegated tokens contain `delegatedBy=payload`,
cannot delegate again, and never receive observe or update capabilities.

The payload root consumes generated topology and performs one delegation request
per child subject. It passes only that child's launch envelope to the child
process. No cell-wide token is placed in the payload tree or shared between web,
API, and the root runtime.

## Steady-state transition

`POST /v1/update/transition` currently accepts only `prepare-latest`. The
runtime-role capability may call it; delegated app tokens and the owner control
token may not. The broker serializes requests and invokes a B0-owned handler.
B0 alone fetches root/release metadata, verifies, downloads, promotes, journals,
and prepares activation state. A `prepared` response includes the canonical
version and `restartRequired=true`; lifecycle shutdown then lets B0 hand off the
candidate. Confirmation and rollback are never caller-selected HTTP actions.
