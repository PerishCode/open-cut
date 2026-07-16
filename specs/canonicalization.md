# Canonical identity and digest
Status: Business design baseline.

## Durable identity

Every durable product entity ID is a service-generated RFC 9562 UUIDv7 encoded
as the canonical lowercase hyphenated string. Go uses kind-specific wrapper
types such as `ProjectID`, `SequenceID`, and `ClipID`; Contracts uses matching
branded strings. A generic string or one entity kind cannot be passed where
another kind is required.

UUID timestamp order is an allocation property only. Creative order,
pagination, conflict detection, and authorization never derive semantics from an
ID. Clients cannot select durable IDs. Project names, track labels, paths, and
container indexes remain display/fact fields.

Caller request identity is not an entity ID. It is an opaque, actor-scoped ASCII
string of 1–128 characters accepted only from a conservative command-schema
alphabet. The caller must preserve it for retries; the server stores its exact
bytes and normalized input digest.

Proposal-local symbols match:

```text
^[a-z][a-z0-9_-]{0,63}$
```

They are unique inside one proposal, never survive as durable identity, and
cannot resemble or override a UUID.

## Canonical JSON

Normalized content is serialized with RFC 8785 JSON Canonicalization Scheme
(JCS) over valid UTF-8. Product integer-string scalars are already canonical
strings under `specs/wire-contract.md`. JSON numbers are allowed only for schema
fields explicitly bounded to the interoperable exact range; NaN, infinity,
negative zero, and imprecise numeric inputs are invalid before canonicalization.

Creator text is preserved byte-semantically as decoded Unicode and is not
silently NFC/NFD normalized, folded, trimmed, localized, or case-converted.
Canonically equivalent but differently encoded creator strings therefore remain
different authored content.

Map key order, input whitespace, and insignificant JSON syntax do not affect the
canonical bytes. Array order remains semantic unless the normalizer explicitly
sorts that field by a declared stable key before JCS.

## Domain-separated digest

Every durable digest hashes one versioned envelope:

```text
{
  "domain": <fixed product domain>,
  "schema": <immutable schema version>,
  "payload": <fully normalized content>
}
```

The digest is SHA-256 over the UTF-8 JCS bytes and is encoded as lowercase
`sha256:<64 hex>`. Domain strings are closed constants such as
`open-cut/edit-proposal`, `open-cut/command-request`,
`open-cut/render-plan`, and `open-cut/job-key`. Equal payload bytes in different
domains or schemas must not collide semantically.

Signatures sign a versioned canonical authorization envelope containing the
relevant content digest; they do not reuse the digest bytes as an untyped
message.

Three related digests remain deliberately separate:

- `command-body` covers the strictly decoded, command-schema-normalized request
  DTO before authorization and lets CLI and API prove identical transport input;
- `command-request` covers authenticated actor, command, AppState, request
  identity, authoritative defaults, and normalized logical input before any
  server-generated durable allocation, and is stored for idempotency;
- `edit-proposal` or another effect domain covers the fully expanded immutable
  effect after server allocation and impact classification and is what apply,
  Approval, journal, and audit bind.

An effect digest containing newly allocated IDs cannot be substituted for the
input digest used to detect request-identity reuse. All JSON command bodies are
size-bounded before decode and reject duplicate object keys, invalid UTF-8, and
numbers outside their declared exact schema before any digest is accepted.

## Semantic closure

Before hashing, normalization expands every behavior-affecting default and
resolves every semantic reference required by that domain:

- an EditProposal contains operation defaults, explicit linked/single scope,
  exact final Clip state, explicit split output/group identities, complete
  Alignment remaps, stable order, preconditions, local-ID allocation map, and
  impact classification inputs;
- a RenderPlan contains compiler/schema version, expanded Sequence state,
  selected streams/artifacts, resource/font identities, color/audio/composition
  policy, exact semantic duration, output frame/sample counts, sampling/tail
  policy, and normalized output policy;
- a WorkJob key contains executor and output schema versions, complete input or
  selected producer-job identities, parameters, resolver/compiler versions,
  target identity, and compatibility policy;
- an authorization request contains method/route identity, command schema,
  AppState context and policy snapshot, request identity, grant revision and
  scope digest, and command-body digest.

Machine locale, timezone, map iteration, system font, system media binary,
hardware selection, environment defaults, current directory, or database row
order may not affect behavior outside the digest. If an execution choice is
declared semantically equivalent, verification—not omission—proves equivalence.

## Storage and diagnostics

Journal rows store schema, canonical JSON bytes, and digest. Reads recompute and
verify at migration/invariant checkpoints; normal writes never accept a caller's
canonical bytes as trusted normalization.

Logs may include the domain, schema, and digest but not creator bodies, source
paths, SourceGrant material, secrets, or signatures.

## Harness

- compare repository JCS output with published RFC 8785 vectors;
- prove key/whitespace variation converges and array/text changes do not;
- reject noncanonical integer strings, unsafe numbers, invalid UTF-8, duplicate
  JSON object keys, and non-finite numeric values before digest;
- prove every durable ID is server-generated valid UUIDv7 and IDs never define
  creative ordering;
- snapshot normalized Proposal, RenderPlan, Job key, and authorization envelopes
  and fail when any behavior-affecting default is omitted;
- round-trip journal canonical bytes and verify their stored digest after
  restart and migration.
