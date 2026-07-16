# Go business kernel and command schemas

Status: Business design baseline.

## Ownership

Reusable product business behavior lives in a top-level Go root, separate from
application and cold-start adapters:

```text
product/domain
product/command
product/application
```

- `product/domain` owns entities, value objects, revisions, operation semantics,
  invariants, proposal normalization, inverse construction, and RenderPlan
  compilation inputs.
- `product/command` owns the closed Agent command DTOs, discriminated operation
  union, result envelopes, help metadata, and JSON Schema registry.
- `product/application` owns use cases and ports for persistence, activity,
  jobs, resources, source access, authorization, and clocks/identity.

These packages import no app-private source, sidecar/client package, launcher,
broker, runtime topology, HTTP adapter, SQLite implementation, Electron API, or
generated TypeScript.

## Adapter dependency direction

```text
cmd/cli
  -> product/command
  -> internal/productclient -> product API

apps/api/controller
  -> product/application

apps/api/repository
  -> implements product/application persistence ports

apps/api service/composition and Agent bridge
  -> assemble product/application with platform adapters

packages/openapi
  <- generated from the Huma API

packages/contracts
  -> adapts generated transport into stable Web-facing ports
```

The CLI may import shared command/result types and help registry but never calls
application use cases in process. The product API remains the only writer and
SQLite owner.

## Command schema source

The Go command registry is the sole source for Agent command input and result
schemas. Each leaf command registers:

```text
path and summary
input Go type
result Go type
mutability and request-identity policy
AppState requirements
possible typed statuses/errors
approval classification
bounded examples
command schema version
```

The edit operation union is closed and discriminator-based. Adding a variant
requires its Go value type, normalization/apply/inverse behavior, generated JSON
Schema, help exposure, persistence encoding, and harness coverage in one change.
`bind-alignment` accepts the unified typed target union; normalization emits one
`put-alignment` operation with a canonical, homogeneous target set. Target-family
specific bind operations are not separate command variants.

The registry renders the normative JSON discovery documents returned by
`--help`. Handwritten JSON Schema files, command schemas inside prompts, and a
second CLI-only DTO hierarchy are forbidden.

## API and OpenAPI relationship

Huma controllers bind transport request/response types built from the same
product command and application DTOs. Huma remains the authoritative producer of
the product HTTP OpenAPI document; no handwritten OpenAPI or parallel product
TypeSpec is added.

Generated `packages/openapi` remains an internal transport adapter. Its schema
must contain the same discriminators and fields as the Go command registry where
the API exposes those operations. A generation check compares canonical schema
fingerprints and rejects drift.

Not every CLI command maps one-to-one to an HTTP endpoint. CLI-only composition
such as bounded wait may use several product API calls, but it cannot invent a
business DTO or operation unavailable from the shared registry.

## Web Contracts boundary

`packages/contracts` continues to own stable Web-facing ports, runtime
validation, reconciliation, and React hooks. It may translate generated OpenAPI
types into narrower immutable contract types; Web never imports product OpenAPI
or Go-generated artifacts directly.

Contracts also owns branded exact scalar strings and checked `bigint`
conversion helpers from `specs/wire-contract.md`. Web business code never
receives a raw OpenAPI `number` for time numerator, revision, or activity cursor.

That deliberate adapter is not a second business-schema authority. Contract
tests validate every mapped discriminator, revision, status, and required field
against generated transport fixtures and reject an unknown operation instead of
silently dropping it.

## Validation order

Every write crosses the same order:

1. decode the versioned command DTO;
2. validate structural JSON Schema constraints;
3. resolve AppState and authorization context;
4. normalize symbolic references, times, and operation defaults;
5. compute normalized input/proposal digest;
6. execute application/domain precondition and invariant checks;
7. persist through one authoritative application use case.

The API repeats authoritative normalization even when the versioned CLI already
validated locally. A compromised or old client cannot bypass domain validation.

Canonical JSON, domain separation, digest closure, durable UUIDv7 allocation,
request identities, and proposal-local symbols follow
`specs/canonicalization.md`. The registry declares mutation budgets and rejects
implicit unbounded expansion.

Entity references in create graphs use the command registry's closed
`durable-id | transaction-local-id` union. Local symbols are proposal-scoped and
are resolved by the application identity port as specified in
`specs/persistence.md`.

## Versioning

Command schema versions belong to the active payload and appear in help,
requests, proposal records, operation journal rows, and results. Within a
release, schema and discriminators are immutable.

Forward migrations may teach the current kernel to read older journal schemas,
but the stable resolver never dispatches an old writer after activation moves.
Unsupported historic operation content fails startup migration or explicit
inspection; it is never guessed.

## Harness assertions

- CLI help schemas are emitted only from the registered Go command types.
- Huma/OpenAPI fingerprints match exposed command DTO fingerprints.
- Every operation variant has normalize, apply, inverse, persistence round-trip,
  API, Contracts, and CLI examples.
- `cmd/cli` and `product/*` do not import `apps/api`.
- Product packages do not import control-plane or platform UI packages.
- An unknown discriminator fails closed at every adapter.
