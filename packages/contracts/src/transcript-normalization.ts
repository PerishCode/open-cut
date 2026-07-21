import { cursorString, type DurableID, durableID, revisionString, uint64String } from "./exact.js";
import type {
  TranscriptArtifact,
  TranscriptCorrection,
  TranscriptDefaultSelection,
  TranscriptReadPage,
  TranscriptSegment,
  TranscriptToken,
} from "./media.js";
import { asRecord, isBoundedInteger, isString, normalizeRational, timestamp } from "./media-validation.js";
import type { RationalTime } from "./projects.js";

const afterPattern = /^(0|[1-9][0-9]{0,9})$/;
const modelNamePattern = /^[a-z][a-z0-9.-]{0,127}$/;

export function transcriptAfter(value: string): string {
  if (!afterPattern.test(value) || BigInt(value) > 4_294_967_295n) {
    throw new Error("transcript cursor is invalid");
  }
  return value;
}

export function transcriptLimit(value: number): number {
  if (!isBoundedInteger(value, 1, 50)) throw new Error("transcript segment limit is invalid");
  return value;
}

export function normalizeTranscriptReadPage(
  value: unknown,
  expectedAssetId: DurableID,
  after?: string,
): TranscriptReadPage {
  const page = asRecord(value);
  if (
    page.schema !== "open-cut/transcript-read/v1" ||
    !Array.isArray(page.segments) ||
    page.segments.length > 50 ||
    !Array.isArray(page.corrections) ||
    page.corrections.length > 256
  ) {
    throw new Error("transcript page is invalid");
  }
  const artifact = normalizeArtifact(page.artifact, expectedAssetId);
  const segments: TranscriptSegment[] = [];
  let expectedOrdinal = after === undefined ? 0 : Number(BigInt(after) + 1n);
  let tokenCount = 0;
  for (const item of page.segments) {
    const segment = normalizeSegment(item, expectedOrdinal);
    tokenCount += segment.tokens.length;
    if (tokenCount > 2_048) throw new Error("transcript page includes too many tokens");
    const previous = segments.at(-1);
    if (previous && compareStartToEnd(segment.sourceRange.start, previous.sourceRange) < 0) {
      throw new Error("transcript segments overlap");
    }
    segments.push(segment);
    expectedOrdinal += 1;
  }
  const nextAfter = page.nextAfter === undefined ? undefined : transcriptAfter(readString(page.nextAfter, 1, 10));
  const last = segments.at(-1);
  if (nextAfter !== undefined && (!last || nextAfter !== String(last.ordinal))) {
    throw new Error("transcript continuation is invalid");
  }
  const corrections = page.corrections.map(normalizeCorrection);
  if (new Set(corrections.map((correction) => correction.id)).size !== corrections.length) {
    throw new Error("transcript corrections are not unique");
  }
  for (let index = 1; index < corrections.length; index += 1) {
    const correction = corrections[index];
    const previous = corrections[index - 1];
    if (correction && previous && compareRational(correction.sourceRange.start, previous.sourceRange.start) < 0) {
      throw new Error("transcript corrections are not ordered");
    }
  }
  return {
    schema: "open-cut/transcript-read/v1",
    artifact,
    segments,
    corrections,
    ...(nextAfter === undefined ? {} : { nextAfter }),
    activityCursor: cursorString(page.activityCursor),
  };
}

function normalizeCorrection(value: unknown): TranscriptCorrection {
  const correction = asRecord(value);
  if (
    !Array.isArray(correction.segmentIds) ||
    correction.segmentIds.length === 0 ||
    correction.segmentIds.length > 256
  ) {
    throw new Error("transcript correction segments are invalid");
  }
  const segmentIds = correction.segmentIds.map(durableID);
  if (new Set(segmentIds).size !== segmentIds.length) {
    throw new Error("transcript correction segments are not unique");
  }
  return {
    id: durableID(correction.id),
    revision: revisionString(correction.revision),
    segmentIds,
    sourceRange: normalizePositiveRange(correction.sourceRange),
    originalText: readString(correction.originalText, 1, 262_144),
    effectiveText: readString(correction.effectiveText, 1, 262_144),
    language: canonicalLanguage(correction.language),
  };
}

export function normalizeTranscriptDefaultSelection(
  value: unknown,
  expectedAssetId: DurableID,
  requestedArtifactId: DurableID,
  expectedPreviousArtifactId: DurableID,
): TranscriptDefaultSelection {
  const selection = asRecord(value);
  const assetId = durableID(selection.assetId);
  const artifactId = durableID(selection.artifactId);
  const previousArtifactId = durableID(selection.previousArtifactId);
  if (assetId !== expectedAssetId || artifactId !== requestedArtifactId || typeof selection.replayed !== "boolean") {
    throw new Error("transcript default selection receipt is invalid");
  }
  if (
    (selection.replayed && previousArtifactId !== artifactId) ||
    (!selection.replayed && previousArtifactId !== expectedPreviousArtifactId)
  ) {
    throw new Error("transcript default selection precondition is invalid");
  }
  return {
    assetId,
    artifactId,
    previousArtifactId,
    selectedAt: timestamp(selection.selectedAt),
    activityCursor: cursorString(selection.activityCursor),
    replayed: selection.replayed,
  };
}

function normalizeArtifact(value: unknown, expectedAssetId: DurableID): TranscriptArtifact {
  const artifact = asRecord(value);
  const assetId = durableID(artifact.assetId);
  if (assetId !== expectedAssetId || artifact.recognitionProfile !== "whisper-small-multilingual-v1") {
    throw new Error("transcript artifact identity is invalid");
  }
  const detectedLanguage = canonicalLanguage(artifact.detectedLanguage);
  const languageConfidenceBasisPoints = optionalConfidence(artifact.languageConfidenceBasisPoints);
  return {
    id: durableID(artifact.id),
    assetId,
    sourceStreamId: durableID(artifact.sourceStreamId),
    recognitionProfile: "whisper-small-multilingual-v1",
    engineVersion: readString(artifact.engineVersion, 1, 1_024),
    engineTarget: readString(artifact.engineTarget, 1, 128),
    modelName: readPattern(artifact.modelName, modelNamePattern, "transcript model name"),
    modelVersion: readString(artifact.modelVersion, 1, 128),
    detectedLanguage,
    ...(languageConfidenceBasisPoints === undefined ? {} : { languageConfidenceBasisPoints }),
    sourceStartTime: normalizeRational(artifact.sourceStartTime),
    normalizedSampleCount: uint64String(artifact.normalizedSampleCount),
    isDefault: readBoolean(artifact.isDefault),
    createdAt: timestamp(artifact.createdAt),
  };
}

function normalizeSegment(value: unknown, expectedOrdinal: number): TranscriptSegment {
  const segment = asRecord(value);
  if (segment.ordinal !== expectedOrdinal || !Array.isArray(segment.tokens) || segment.tokens.length > 2_048) {
    throw new Error("transcript segment order is invalid");
  }
  const sourceRange = normalizePositiveRange(segment.sourceRange);
  const tokens = segment.tokens.map((token) => normalizeToken(token, sourceRange));
  const text = readString(segment.text, 1, 8_192);
  if (tokens.length === 0 || tokens.map((token) => token.text).join("") !== text) {
    throw new Error("transcript token evidence is invalid");
  }
  for (let index = 1; index < tokens.length; index += 1) {
    const token = tokens[index];
    const previous = tokens[index - 1];
    if (token && previous && compareStartToEnd(token.sourceRange.start, previous.sourceRange) < 0) {
      throw new Error("transcript tokens overlap");
    }
  }
  return { id: durableID(segment.id), ordinal: expectedOrdinal, sourceRange, text, tokens };
}

function normalizeToken(value: unknown, segmentRange: TimeRange): TranscriptToken {
  const token = asRecord(value);
  const sourceRange = normalizePositiveRange(token.sourceRange);
  if (compareRational(sourceRange.start, segmentRange.start) < 0 || compareRangeEnds(sourceRange, segmentRange) > 0) {
    throw new Error("transcript token range escapes its segment");
  }
  const confidenceBasisPoints = optionalConfidence(token.confidenceBasisPoints);
  return {
    id: durableID(token.id),
    sourceRange,
    text: readTokenText(token.text),
    ...(confidenceBasisPoints === undefined ? {} : { confidenceBasisPoints }),
  };
}

type TimeRange = Readonly<{ start: RationalTime; duration: RationalTime }>;

function normalizePositiveRange(value: unknown): TimeRange {
  const range = asRecord(value);
  const start = normalizeRational(range.start);
  const duration = normalizeRational(range.duration);
  if (BigInt(duration.value) <= 0n) throw new Error("transcript time range is not positive");
  return { start, duration };
}

function compareStartToEnd(start: RationalTime, range: TimeRange): number {
  const end = rangeEndFraction(range);
  return compareFractions(BigInt(start.value), BigInt(start.scale), end.numerator, end.denominator);
}

function compareRational(left: RationalTime, right: RationalTime): number {
  return compareFractions(BigInt(left.value), BigInt(left.scale), BigInt(right.value), BigInt(right.scale));
}

function compareRangeEnds(left: TimeRange, right: TimeRange): number {
  const leftEnd = rangeEndFraction(left);
  const rightEnd = rangeEndFraction(right);
  return compareFractions(leftEnd.numerator, leftEnd.denominator, rightEnd.numerator, rightEnd.denominator);
}

function rangeEndFraction(range: TimeRange): Readonly<{ numerator: bigint; denominator: bigint }> {
  return {
    numerator:
      BigInt(range.start.value) * BigInt(range.duration.scale) +
      BigInt(range.duration.value) * BigInt(range.start.scale),
    denominator: BigInt(range.start.scale) * BigInt(range.duration.scale),
  };
}

function compareFractions(
  leftNumerator: bigint,
  leftDenominator: bigint,
  rightNumerator: bigint,
  rightDenominator: bigint,
): number {
  const difference = leftNumerator * rightDenominator - rightNumerator * leftDenominator;
  return difference < 0n ? -1 : difference > 0n ? 1 : 0;
}

function optionalConfidence(value: unknown): number | undefined {
  if (value === undefined) return undefined;
  if (!isBoundedInteger(value, 0, 10_000)) throw new Error("transcript confidence is invalid");
  return value;
}

function canonicalLanguage(value: unknown): string {
  const language = readString(value, 1, 64);
  if (language.includes("-u-") || language.includes("-t-") || language.includes("-x-")) {
    throw new Error("transcript language extensions are not allowed");
  }
  try {
    if (Intl.getCanonicalLocales(language)[0] !== language) throw new Error("not canonical");
  } catch {
    throw new Error("transcript language is not canonical BCP-47");
  }
  return language;
}

function readString(value: unknown, minimum: number, maximum: number): string {
  if (!isString(value, minimum, maximum) || value.trim() !== value) throw new Error("transcript text is invalid");
  return value;
}

// Token text is lexical, not display text: whitespace inside it carries word
// boundaries. Whisper emits leading spaces on most tokens, and the segment check
// above requires the tokens to concatenate back into the segment exactly, so
// trimming here would make the two rules contradict each other and reject every
// real transcript. Only the surrounding segment text is required to be trimmed.
function readTokenText(value: unknown): string {
  if (!isString(value, 1, 512)) throw new Error("transcript token text is invalid");
  return value;
}

function readPattern(value: unknown, pattern: RegExp, label: string): string {
  if (typeof value !== "string" || !pattern.test(value)) throw new Error(`${label} is invalid`);
  return value;
}

function readBoolean(value: unknown): boolean {
  if (typeof value !== "boolean") throw new Error("transcript default state is invalid");
  return value;
}
