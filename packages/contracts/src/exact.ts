declare const int64Brand: unique symbol;
declare const uint64Brand: unique symbol;
declare const revisionBrand: unique symbol;
declare const cursorBrand: unique symbol;
declare const digestBrand: unique symbol;
declare const durableIDBrand: unique symbol;

export type Int64String = string & { readonly [int64Brand]: true };
export type UInt64String = string & { readonly [uint64Brand]: true };
export type RevisionString = UInt64String & { readonly [revisionBrand]: true };
export type CursorString = UInt64String & { readonly [cursorBrand]: true };
export type DigestString = string & { readonly [digestBrand]: true };
export type DurableID = string & { readonly [durableIDBrand]: true };

const signedPattern = /^(0|-[1-9][0-9]*|[1-9][0-9]*)$/;
const unsignedPattern = /^(0|[1-9][0-9]*)$/;
const digestPattern = /^sha256:[0-9a-f]{64}$/;
const uuidV7Pattern = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const minimumInt64 = -(1n << 63n);
const maximumInt64 = (1n << 63n) - 1n;

export function int64String(value: unknown): Int64String {
  if (typeof value !== "string" || !signedPattern.test(value)) throw new Error("expected canonical int64 string");
  const parsed = BigInt(value);
  if (parsed < minimumInt64 || parsed > maximumInt64) throw new Error("int64 string is out of range");
  return value as Int64String;
}

export function revisionString(value: unknown): RevisionString {
  return boundedUInt64(value, "revision") as RevisionString;
}

export function uint64String(value: unknown): UInt64String {
  return boundedUInt64(value, "uint64");
}

export function cursorString(value: unknown): CursorString {
  return boundedUInt64(value, "cursor") as CursorString;
}

export function digestString(value: unknown): DigestString {
  if (typeof value !== "string" || !digestPattern.test(value)) throw new Error("expected sha256 digest");
  return value as DigestString;
}

export function durableID(value: unknown): DurableID {
  if (typeof value !== "string" || !uuidV7Pattern.test(value)) throw new Error("expected canonical UUIDv7");
  return value as DurableID;
}

export function exactBigInt(value: Int64String | UInt64String): bigint {
  return BigInt(value);
}

export function compareExact(left: Int64String | UInt64String, right: Int64String | UInt64String): -1 | 0 | 1 {
  const leftValue = BigInt(left);
  const rightValue = BigInt(right);
  return leftValue < rightValue ? -1 : leftValue > rightValue ? 1 : 0;
}

export function incrementCursor(value: CursorString): CursorString {
  const next = BigInt(value) + 1n;
  if (next > maximumInt64) throw new Error("cursor overflow");
  return next.toString() as CursorString;
}

function boundedUInt64(value: unknown, label: string): UInt64String {
  if (typeof value !== "string" || !unsignedPattern.test(value)) {
    throw new Error(`expected canonical ${label} string`);
  }
  const parsed = BigInt(value);
  if (parsed > maximumInt64) throw new Error(`${label} string is out of range`);
  return value as UInt64String;
}
