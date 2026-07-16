import { type DurableID, type Int64String, int64String, type RevisionString } from "./exact.js";
import type { TranscriptArtifact, TranscriptCorrection, TranscriptSegment, TranscriptToken } from "./media.js";
import type { RationalTime } from "./projects.js";

export type CreatorSourceExcerptEvidence = Readonly<{
  artifactId: DurableID;
  assetId: DurableID;
  sourceStreamId: DurableID;
  language: string;
  segmentIds: readonly DurableID[];
  selectedTokenIds: readonly DurableID[];
  sourceRange: Readonly<{ start: RationalTime; duration: RationalTime }>;
  correctionRevisions: readonly Readonly<{ id: DurableID; revision: RevisionString }>[];
}>;

type IndexedToken = Readonly<{ segment: TranscriptSegment; token: TranscriptToken }>;

export function selectSourceExcerptEvidence(input: {
  artifact: TranscriptArtifact;
  segments: readonly TranscriptSegment[];
  corrections: readonly TranscriptCorrection[];
  anchorTokenId: DurableID;
  focusTokenId: DurableID;
}): CreatorSourceExcerptEvidence {
  const tokens: IndexedToken[] = [];
  for (let index = 0; index < input.segments.length; index += 1) {
    const segment = input.segments[index];
    if (!segment) continue;
    const previous = input.segments[index - 1];
    if (previous && segment.ordinal !== previous.ordinal + 1) {
      throw new Error("SourceExcerpt segments are not contiguous");
    }
    for (const token of segment.tokens) tokens.push({ segment, token });
  }
  const anchor = tokens.findIndex((candidate) => candidate.token.id === input.anchorTokenId);
  const focus = tokens.findIndex((candidate) => candidate.token.id === input.focusTokenId);
  if (anchor < 0 || focus < 0) throw new Error("SourceExcerpt token selection is outside the loaded transcript");
  const first = Math.min(anchor, focus);
  const last = Math.max(anchor, focus);
  const selected = tokens.slice(first, last + 1);
  const firstToken = selected[0]?.token;
  const lastToken = selected.at(-1)?.token;
  if (!firstToken || !lastToken) throw new Error("SourceExcerpt token selection is empty");

  const segmentIds: DurableID[] = [];
  for (const item of selected) {
    if (segmentIds.at(-1) !== item.segment.id) segmentIds.push(item.segment.id);
  }
  if (segmentIds.length > 256) throw new Error("SourceExcerpt segment selection exceeds its budget");
  const start = firstToken.sourceRange.start;
  const end = addRational(lastToken.sourceRange.start, lastToken.sourceRange.duration);
  const duration = subtractRational(end, start);
  if (BigInt(duration.value) <= 0n) throw new Error("SourceExcerpt token selection has no duration");
  const range = { start, duration };

  const correctionRevisions: Array<Readonly<{ id: DurableID; revision: RevisionString }>> = [];
  for (const correction of input.corrections) {
    if (correction.language !== input.artifact.detectedLanguage) continue;
    const correctionEnd = addRational(correction.sourceRange.start, correction.sourceRange.duration);
    if (!rangesOverlap(start, end, correction.sourceRange.start, correctionEnd)) continue;
    if (compareRational(correction.sourceRange.start, start) < 0 || compareRational(correctionEnd, end) > 0) {
      throw new Error("SourceExcerpt selection cuts through a TranscriptCorrection");
    }
    correctionRevisions.push({ id: correction.id, revision: correction.revision });
  }
  if (correctionRevisions.length > 256) throw new Error("SourceExcerpt correction selection exceeds its budget");
  return {
    artifactId: input.artifact.id,
    assetId: input.artifact.assetId,
    sourceStreamId: input.artifact.sourceStreamId,
    language: input.artifact.detectedLanguage,
    segmentIds,
    selectedTokenIds: selected.map((item) => item.token.id),
    sourceRange: range,
    correctionRevisions,
  };
}

function rangesOverlap(
  leftStart: RationalTime,
  leftEnd: RationalTime,
  rightStart: RationalTime,
  rightEnd: RationalTime,
) {
  return compareRational(leftStart, rightEnd) < 0 && compareRational(rightStart, leftEnd) < 0;
}

function compareRational(left: RationalTime, right: RationalTime): number {
  const difference = BigInt(left.value) * BigInt(right.scale) - BigInt(right.value) * BigInt(left.scale);
  return difference < 0n ? -1 : difference > 0n ? 1 : 0;
}

function addRational(left: RationalTime, right: RationalTime): RationalTime {
  return rational(
    BigInt(left.value) * BigInt(right.scale) + BigInt(right.value) * BigInt(left.scale),
    BigInt(left.scale) * BigInt(right.scale),
  );
}

function subtractRational(left: RationalTime, right: RationalTime): RationalTime {
  return rational(
    BigInt(left.value) * BigInt(right.scale) - BigInt(right.value) * BigInt(left.scale),
    BigInt(left.scale) * BigInt(right.scale),
  );
}

function rational(numerator: bigint, denominator: bigint): RationalTime {
  const divisor = greatestCommonDivisor(numerator, denominator);
  const value = numerator / divisor;
  const scale = denominator / divisor;
  if (scale < 1n || scale > 2_147_483_647n) throw new Error("SourceExcerpt rational scale exceeds its budget");
  return { value: int64String(value.toString()) as Int64String, scale: Number(scale) };
}

function greatestCommonDivisor(left: bigint, right: bigint): bigint {
  let a = left < 0n ? -left : left;
  let b = right;
  while (b !== 0n) [a, b] = [b, a % b];
  return a;
}
