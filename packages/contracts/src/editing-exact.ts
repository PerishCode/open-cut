import { int64String } from "./exact.js";
import type { RationalTime } from "./projects.js";

export type TimeRange = Readonly<{ start: RationalTime; duration: RationalTime }>;

export function timeRangeKey(range: TimeRange): string {
  return `${range.start.value}/${range.start.scale}\u0000${range.duration.value}/${range.duration.scale}`;
}

export function normalizeTimeRange(value: unknown): TimeRange {
  const range = asRecord(value);
  const start = normalizeRational(range.start);
  const duration = normalizeRational(range.duration);
  if (BigInt(duration.value) <= 0n) throw new Error("time range duration is invalid");
  return { start, duration };
}

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

function greatestCommonDivisor(left: bigint, right: bigint): bigint {
  let a = left < 0n ? -left : left;
  let b = right;
  while (b !== 0n) [a, b] = [b, a % b];
  return a;
}

function isBoundedInteger(value: unknown, minimum: number, maximum: number): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= minimum && value <= maximum;
}

export function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("product payload is invalid");
  return value as Record<string, unknown>;
}

export function canonicalLanguage(value: unknown, label: string): string {
  if (typeof value !== "string" || value.length === 0 || value.length > 64) {
    throw new Error(`${label} language is invalid`);
  }
  if (value.includes("-u-") || value.includes("-t-") || value.includes("-x-")) {
    throw new Error(`${label} language extensions are not allowed`);
  }
  try {
    if (Intl.getCanonicalLocales(value)[0] !== value) throw new Error("not canonical");
  } catch {
    throw new Error(`${label} language is not canonical BCP-47`);
  }
  return value;
}
