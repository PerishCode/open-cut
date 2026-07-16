# Local product authorization

Status: Business design baseline.

## Boundaries

Loopback is transport locality, not business authorization. Broker observer
capability discovers current product endpoints but cannot authorize Project
reads or writes. The Agent-facing CLI uses a separate OS-user-and-installation
grant whose private material never enters Agent context.

Lifecycle owns platform secure-storage mechanics only. The product API owns
pairing state, scopes, revocation, request verification, approvals, and audit.

## Installation key

The installed stable CLI uses one installation-scoped Ed25519 keypair:

- lifecycle platform adapters create or load the private key from OS secure
  storage;
- the private key is non-exportable where the platform supports it and is never
  written to environment, settings, receipt, bootstrap, product database, log,
  help, stdout, or Agent scratch;
- the API stores grant ID, public key, OS-user/install identity metadata, scopes,
  creation time, and revocation state in its SQLite data;
- reinstall/repair preserves or deliberately rotates the key through lifecycle;
  uninstall and creator revocation converge without leaving an active API grant.

Packaged operation requires an approved secure-storage adapter. Development uses
an isolated, permission-restricted key below the guarded `.tmp/oc-control` cell
and never registers it as a production grant.

The packaged platform signer is callable only through an attested process
chain. A `product-cli` request must descend from the receipt-owned stable CLI
resolver. A `first-party-ui` request must come directly from a signed active app
below a lifecycle-selected release root and its ancestry must include the
receipt-owned platform host. On macOS, lifecycle compares the kernel-reported
process executable identity, not `argv[0]` or display command text; invoking the
stable CLI normally through `PATH` must preserve the same trust decision. These
checks authorize access to the role key only and do not create product scope or
business authority.

## Pairing

The first signed business attempt from an unknown public key creates a bounded
pending pairing request after proving key possession against an API nonce. The
request exposes only creator-readable installation/CLI identity and requested
scope.

The Open Cut UI shows the pending request. Creator approval activates that exact
public key and scope; denial or expiry leaves it unusable. The Agent can observe
`pairing-required` and wait through CLI business status but cannot approve,
replace the public key, or widen scope.

Pairing endpoints are loopback-only, rate-limited, nonce-bound, and cannot mutate
creative state. A public key by itself is not authority before UI activation.

## Signed CLI request

For each business call, the versioned CLI obtains a short-lived single-use API
challenge and signs a versioned canonical request containing at least:

```text
grantId
grant revision and canonical active-scope digest
challenge nonce and expiry
HTTP method and product route identity
command schema version
resolved project/run/turn context and invocation-policy snapshot
command-body digest
request identity when mutating
```

The API atomically consumes the challenge, verifies signature and active scope,
then performs normal AppState relationship, turn-generation, revision,
idempotency, proposal, and approval checks. Replaying a signature or changing
any canonical field fails before business execution.

Signature headers and challenge exchange are internal CLI transport behavior.
They are not described in Agent help or returned envelopes.

## Scope and approval

Each Agent-visible command declares one required scope in the command registry;
the API, challenge, pairing display, and signed request all bind that same value.
A new pairing presents the exact sorted current registry scope set to the
Creator, including the read-only `product:read` scope used by `product status`.
An existing grant never inherits a newly shipped scope. Using a later command
therefore requires an explicit Creator-visible scope-upgrade flow before that
command can become authorized. No grant ever bypasses exact durable Approval for
protected effects.

A scope upgrade keeps the installation key, grant, and durable Agent principal.
When an otherwise active grant lacks a registry scope, the API creates or reuses
one bounded pending upgrade containing the union of its current scopes and the
required scopes, and returns `scope-upgrade-required`. The old scope set remains
fully active while the upgrade is pending; denial or expiry never revokes it.

Creator approval atomically installs the exact requested scope set, increments
the grant revision, and records the decision. Every challenge and signature bind
that revision plus the digest of the sorted active scope set, so approval
invalidates outstanding challenges made against the old authority. Scope upgrade
approval is distinct from durable effect Approval and cannot consume or imply
one.

Approval authorizes a normalized proposal/action digest, not a larger CLI scope.
Revoking the installation grant immediately prevents new commands and late
AgentTurn writes; it does not undo committed transactions or corrupt running
jobs already owned by the product.

## First-party UI session

Web/Electron product calls use a separate first-party session established by the
installed application. Browser code never receives the CLI installation private
key, broker token, or Agent grant. Direct loopback requests without either a
valid first-party session or signed CLI request are rejected.

Lifecycle provisions a separate `first-party-ui` role key; reusing the
`product-cli` key is forbidden. Electron main proves possession to an API
single-use challenge, retains the resulting short-lived session only in main
memory, and injects it through the `oc://` and Web proxy chain. The exact
instance binding, expiry, development adapter, and renderer-unreadable transport
are defined in `specs/ui-session.md` and preserve
Web -> Contracts -> transport/proxy -> API.

## Logging and errors

Authorization logs identify grant ID, command identity, outcome, and redacted
request digest. They never include private keys, signatures, challenges,
SourceGrant material, original paths, or request bodies containing creator
content.

Agent-visible errors distinguish pairing required, grant revoked, scope denied,
challenge expired, and product unavailable without revealing verifier details.

## Harness

- unknown keys cannot read or write product state;
- proof-of-possession cannot self-approve pairing;
- nonce/signature replay and canonical-field changes are rejected;
- one installation cannot use another installation's grant;
- env/argv context never changes authenticated actor identity or scope;
- grant revocation rejects a still-running AgentTurn's next write;
- pending scope upgrade preserves old reads, while the new command remains
  denied until Creator approval; approval invalidates an old signed challenge;
- secure material never appears in help, stdout, stderr evidence, process env,
  receipt, data paths, or Agent scratch;
- direct loopback HTTP without a recognized UI or CLI authority fails closed.
