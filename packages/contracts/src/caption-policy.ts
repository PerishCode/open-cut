import { asRecord, normalizeRational } from "./editing-exact.js";
import type { RationalTime } from "./projects.js";

export type CaptionDerivationPolicy = Readonly<{
  id: "readable-captions-v1";
  maximumLines: 2;
  maximumLineGraphemes: 42;
  minimumDuration: RationalTime;
  maximumDuration: RationalTime;
  maximumGap: RationalTime;
  maximumReadingRate: 20;
  boundaryPolicy: "terminal-punctuation-v1";
  timingPolicy: "forward-pad-no-overlap-v1";
  unicodeSegmentationId: "unicode-egc-15.0.0-uniseg-v0.4.7";
}>;

export function normalizeCaptionDerivationPolicy(value: unknown): CaptionDerivationPolicy {
  const policy = asRecord(value);
  const minimumDuration = normalizeRational(policy.minimumDuration);
  const maximumDuration = normalizeRational(policy.maximumDuration);
  const maximumGap = normalizeRational(policy.maximumGap);
  if (
    policy.id !== "readable-captions-v1" ||
    policy.maximumLines !== 2 ||
    policy.maximumLineGraphemes !== 42 ||
    policy.maximumReadingRate !== 20 ||
    policy.boundaryPolicy !== "terminal-punctuation-v1" ||
    policy.timingPolicy !== "forward-pad-no-overlap-v1" ||
    policy.unicodeSegmentationId !== "unicode-egc-15.0.0-uniseg-v0.4.7" ||
    !isExactRational(minimumDuration, "1", 1) ||
    !isExactRational(maximumDuration, "6", 1) ||
    !isExactRational(maximumGap, "3", 4)
  ) {
    throw new Error("Caption derivation policy is invalid");
  }
  return {
    id: "readable-captions-v1",
    maximumLines: 2,
    maximumLineGraphemes: 42,
    minimumDuration,
    maximumDuration,
    maximumGap,
    maximumReadingRate: 20,
    boundaryPolicy: "terminal-punctuation-v1",
    timingPolicy: "forward-pad-no-overlap-v1",
    unicodeSegmentationId: "unicode-egc-15.0.0-uniseg-v0.4.7",
  };
}

function isExactRational(value: RationalTime, numerator: string, scale: number): boolean {
  return value.value === numerator && value.scale === scale;
}
