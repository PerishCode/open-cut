import { int64String } from "./exact.js";
import type { RationalTime } from "./projects.js";

type MediaType = "video" | "audio" | "subtitle" | "data" | "attachment" | "other";

export function normalizeRational(value: unknown): RationalTime {
  const rational = asRecord(value);
  if (!isBoundedInteger(rational.scale, 1, 2_147_483_647)) throw new Error("rational scale is invalid");
  const numerator = int64String(rational.value);
  const divisor = greatestCommonDivisor(BigInt(numerator), BigInt(rational.scale));
  if ((numerator === "0" && rational.scale !== 1) || (numerator !== "0" && divisor !== 1n)) {
    throw new Error("rational value is not normalized");
  }
  return { value: numerator, scale: rational.scale };
}

export function validateLimit(value: number): number {
  if (!isBoundedInteger(value, 1, 100)) throw new Error("Asset page limit is invalid");
  return value;
}

export async function readJSON(response: Response): Promise<unknown> {
  const text = await response.text();
  if (!text) return {};
  try {
    return JSON.parse(text) as unknown;
  } catch {
    throw new Error(`response ${response.status} is not JSON`);
  }
}

export async function responseError(action: string, status: number, value: unknown): Promise<Error> {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    const record = value as Record<string, unknown>;
    const detail =
      typeof record.detail === "string"
        ? record.detail
        : typeof record.message === "string"
          ? record.message
          : undefined;
    if (detail) return new Error(`${action} returned ${status}: ${detail}`);
  }
  return new Error(`${action} returned ${status}`);
}

export function timestamp(value: unknown): string {
  if (typeof value !== "string" || Number.isNaN(Date.parse(value))) throw new Error("timestamp is invalid");
  return value;
}

export function optionalInteger<const Key extends string>(
  record: Record<string, unknown>,
  key: Key,
  minimum: number,
  maximum: number,
): Partial<Record<Key, number>> {
  const value = record[key];
  if (value === undefined) return {};
  if (!isBoundedInteger(value, minimum, maximum)) throw new Error(`source stream ${key} is invalid`);
  return { [key]: value } as Partial<Record<Key, number>>;
}

export function optionalText<const Key extends string>(
  record: Record<string, unknown>,
  key: Key,
  maximum: number,
): Partial<Record<Key, string>> {
  const value = record[key];
  if (value === undefined) return {};
  if (!isString(value, 0, maximum)) throw new Error(`source stream ${key} is invalid`);
  return { [key]: value } as Partial<Record<Key, string>>;
}

export function normalizeTextList(
  value: readonly unknown[],
  maximum: number,
  requireSorted: boolean,
): readonly string[] {
  const result = value.map((item) => {
    if (!isString(item, 1, maximum)) throw new Error("media text list is invalid");
    return item;
  });
  if (new Set(result).size !== result.length) throw new Error("media text list contains duplicates");
  if (requireSorted) {
    let previous: string | undefined;
    for (const item of result) {
      if (previous !== undefined && previous > item) throw new Error("media text list is not sorted");
      previous = item;
    }
  }
  return result;
}

function greatestCommonDivisor(left: bigint, right: bigint): bigint {
  let a = left < 0n ? -left : left;
  let b = right;
  while (b !== 0n) [a, b] = [b, a % b];
  return a;
}

export function isBoundedInteger(value: unknown, minimum: number, maximum: number): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= minimum && value <= maximum;
}

export function isString(value: unknown, minimum: number, maximum: number): value is string {
  return typeof value === "string" && value.length >= minimum && value.length <= maximum;
}

export function isMediaType(value: unknown): value is MediaType {
  return (
    value === "video" ||
    value === "audio" ||
    value === "subtitle" ||
    value === "data" ||
    value === "attachment" ||
    value === "other"
  );
}

export function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("product payload is invalid");
  return value as Record<string, unknown>;
}
