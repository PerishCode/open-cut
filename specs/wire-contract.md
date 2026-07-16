# Exact product wire values

Status: Business design baseline.

## Purpose

Product time, revisions, cursors, identities, and digests must round-trip exactly
through Go, JSON, generated OpenAPI clients, Contracts, TypeScript, SQLite, CLI
stdout, and journal payloads. JavaScript's safe-integer limit is never a product
limit and implicit float conversion is forbidden.

## Scalar vocabulary

The shared logical wire vocabulary is:

```text
Int64String    = "0" | "-"? [1-9][0-9]* within signed 64-bit range
UInt64String   = "0" | [1-9][0-9]* within unsigned 64-bit range
RevisionString = UInt64String, additionally limited to SQLite signed-positive range
CursorString   = UInt64String, scoped by its enclosing activity scope
DigestString   = "sha256:" followed by 64 lowercase hexadecimal digits
InstantString  = canonical UTC RFC 3339 with required fractional precision policy
DurableID      = opaque lowercase canonical string for its declared ID kind
```

Leading plus signs, leading zeroes other than `0`, exponent notation, whitespace,
negative zero, JSON numbers, and floats are invalid for the integer-string
types. Parsers range-check before conversion.

Revisions and activity cursors are semantically unsigned monotonic values. The
first SQLite implementation caps them at `9223372036854775807` so they remain
native indexed INTEGER columns; reaching the cap is a hard invariant failure,
not wraparound. Their JSON form is always a decimal string.

## Exact rational and rational time

The wire representation of exact time is:

```json
{
  "value": "1001",
  "scale": 30000
}
```

`value` is `Int64String`; `scale` is a JSON integer in `1..2147483647` and is
therefore exactly representable in JavaScript. Both `ExactRational` and
`RationalTime` use this canonical shape, are reduced by greatest common divisor,
and have the unique zero `{ "value": "0", "scale": 1 }`.

They are distinct domain types. `RationalTime` always denotes seconds.
`ExactRational` denotes a dimensionless ratio such as scale or a normalized
canvas coordinate. Reusing `RationalTime` for geometry, gain, or another scalar
is invalid even though the wire representation is isomorphic. A field's schema
determines which type and unit it carries; callers cannot substitute one for the
other by copying JSON.

Wire decoders accept only normalized form for persisted commands, proposal
digests, results, and journal content. UI draft helpers may construct unreduced
values in memory but must normalize before crossing a port.

## Go and TypeScript representation

Go domain types wrap `int64`, `uint64`, and bounded `int32`; transport marshalers
emit and accept the canonical strings above. Raw numeric aliases are not exposed
in command DTOs.

Contracts owns branded TypeScript strings for exact wire values and explicit
conversion helpers. It may convert to `bigint` for arithmetic and comparison,
but JSON serialization always returns the canonical decimal string. Conversion
to `number` is allowed only after a named checked display conversion proves the
value is in the safe range. Time math never uses `number` seconds.

Generated OpenAPI schemas use `type: string` plus anchored patterns and format
names such as `int64-decimal`, `uint64-decimal`, and `sha256-digest`. A schema
declaring these values as JSON `integer`, `number`, or unformatted string is
drift.

## IDs, timestamps, and ordering

Every field uses a kind-specific opaque ID type; arbitrary strings are not
interchangeable IDs. Durable UUIDv7 syntax, local symbols, request identities,
JCS, and domain-separated hashing are defined in
`specs/canonicalization.md`; all adapters preserve validated exact strings.

Instants describe operational wall-clock facts only. Creative ordering,
playback, media positions, revisions, and activity ordering never depend on a
timestamp. Activity is ordered by its scoped CursorString, with `eventId` used
for idempotent duplicate suppression.

## Persistence and comparison

SQLite stores exact time numerator and scale in INTEGER columns and rejects
invalid scale or range by constraints plus repository validation. Revisions and
cursors use positive INTEGER columns. Canonical JSON stored in proposal and
journal rows contains decimal strings, so reading through JSON never loses
precision.

Comparisons and range arithmetic use checked cross multiplication or a wider
intermediate with explicit overflow handling. Lexicographic comparison of
decimal strings is never numeric ordering.

## Harness

- round-trip minimum, maximum, zero, and adjacent-to-safe-integer values through
  command JSON, Huma, generated TypeScript, Contracts, CLI, SQLite, and journal;
- reject numeric JSON, exponent form, leading zeroes, negative revisions,
  invalid scale, overflow, and unreduced persisted rational values;
- property-test rational comparison and arithmetic against an arbitrary-
  precision oracle;
- scan generated product OpenAPI for exact fields incorrectly emitted as JSON
  numbers;
- prove no Contracts reducer or UI selector coerces a revision, cursor, or time
  numerator to `number`.
