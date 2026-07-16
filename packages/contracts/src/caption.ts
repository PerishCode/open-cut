import { previewCreatorCaptions } from "@open-cut/openapi/creator";

import { type CaptionDerivationPolicy, normalizeCaptionDerivationPolicy } from "./caption-policy.js";
import {
  type CreatorEditCommit,
  type CreatorWireEditBody,
  commitCreatorWireEdit,
  creatorEditResponseError,
  validateCreatorIntent,
  validateCreatorRequestID,
} from "./creator-editing.js";
import { asRecord, canonicalLanguage, normalizeTimeRange, type TimeRange } from "./editing-exact.js";
import {
  type CursorString,
  cursorString,
  type DurableID,
  durableID,
  type RevisionString,
  revisionString,
} from "./exact.js";

export type CreatorCaptionPreviewInput = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  sourceExcerptId: DurableID;
  sourceExcerptRevision: RevisionString;
  clipId: DurableID;
  clipRevision: RevisionString;
  trackId: DurableID;
  trackRevision: RevisionString;
}>;

export type CreatorCaptionCue = Readonly<{
  ordinal: number;
  text: string;
  sourceRange: TimeRange;
  timelineRange: TimeRange;
}>;

export type CreatorCaptionReview = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  baseProjectRevision: RevisionString;
  activityCursor: CursorString;
  language: string;
  policy: CaptionDerivationPolicy;
  sourceExcerptId: DurableID;
  clipId: DurableID;
  trackId: DurableID;
  preconditionCount: number;
  cues: readonly CreatorCaptionCue[];
}>;

export type ApplyCreatorCaptionInput = Readonly<{ requestId: string; intent: string }>;

export interface CreatorCaptionPort {
  preview(input: CreatorCaptionPreviewInput, signal?: AbortSignal): Promise<CreatorCaptionReview>;
  apply(
    review: CreatorCaptionReview,
    input: ApplyCreatorCaptionInput,
    signal?: AbortSignal,
  ): Promise<CreatorEditCommit>;
}

type CaptionEnvelope = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  baseProjectRevision: CreatorWireEditBody["baseProjectRevision"];
  preconditions: CreatorWireEditBody["preconditions"];
  operation: CreatorWireEditBody["operations"][number];
}>;

type CaptionPrecondition = Readonly<{
  kind: "asset" | "narrative-node" | "clip" | "track" | "transcript-correction";
  id: DurableID;
  revision: RevisionString;
}>;

export function createCreatorCaptionPort(): CreatorCaptionPort {
  const envelopes = new WeakMap<CreatorCaptionReview, CaptionEnvelope>();
  return {
    preview: async (input, signal) => {
      const request = normalizePreviewInput(input);
      const response = await previewCreatorCaptions(request.projectId, request.sequenceId, request.body, { signal });
      if (response.status !== 200) throw creatorEditResponseError(response.status);
      const { review, envelope } = normalizeReview(response.data, request);
      envelopes.set(review, envelope);
      return review;
    },
    apply: async (review, input, signal) => {
      const envelope = envelopes.get(review);
      if (!envelope) throw new Error("Creator Caption review is not owned by this Contracts session");
      validateCreatorRequestID(input.requestId);
      validateCreatorIntent(input.intent, false);
      return commitCreatorWireEdit(
        envelope.projectId,
        envelope.sequenceId,
        {
          requestId: input.requestId,
          intent: input.intent,
          baseProjectRevision: envelope.baseProjectRevision,
          preconditions: envelope.preconditions,
          operations: [envelope.operation],
        },
        signal,
      );
    },
  };
}

function normalizePreviewInput(input: CreatorCaptionPreviewInput) {
  const projectId = durableID(input.projectId);
  const sequenceId = durableID(input.sequenceId);
  const sourceExcerptId = durableID(input.sourceExcerptId);
  const sourceExcerptRevision = revisionString(input.sourceExcerptRevision);
  const clipId = durableID(input.clipId);
  const clipRevision = revisionString(input.clipRevision);
  const trackId = durableID(input.trackId);
  const trackRevision = revisionString(input.trackRevision);
  const localPrefix = `cap_${crypto.randomUUID().replaceAll("-", "")}`;
  return {
    projectId,
    sequenceId,
    sourceExcerptId,
    sourceExcerptRevision,
    clipId,
    clipRevision,
    trackId,
    trackRevision,
    localPrefix,
    body: {
      sourceExcerptId,
      sourceExcerptRevision,
      clipId,
      clipRevision,
      trackId,
      trackRevision,
      localPrefix,
    },
  } as const;
}

function normalizeReview(
  value: unknown,
  request: ReturnType<typeof normalizePreviewInput>,
): { review: CreatorCaptionReview; envelope: CaptionEnvelope } {
  const payload = asRecord(value);
  assertExactKeys(
    payload,
    ["activityCursor", "baseProjectRevision", "language", "operation", "preconditions"],
    "Caption preview",
  );
  const preconditions = normalizePreconditions(payload.preconditions);
  requirePrecondition(preconditions, "narrative-node", request.sourceExcerptId, request.sourceExcerptRevision);
  requirePrecondition(preconditions, "clip", request.clipId, request.clipRevision);
  requirePrecondition(preconditions, "track", request.trackId, request.trackRevision);
  const operation = normalizeCaptionOperation(payload.operation, request);
  const review: CreatorCaptionReview = Object.freeze({
    projectId: request.projectId,
    sequenceId: request.sequenceId,
    baseProjectRevision: revisionString(payload.baseProjectRevision),
    activityCursor: cursorString(payload.activityCursor),
    language: canonicalLanguage(payload.language, "Caption review"),
    policy: operation.captionPolicy,
    sourceExcerptId: request.sourceExcerptId,
    clipId: request.clipId,
    trackId: request.trackId,
    preconditionCount: preconditions.length,
    cues: operation.derivedCaptions.map((cue, index) => ({
      ordinal: index + 1,
      text: cue.text,
      sourceRange: cue.sourceRange,
      timelineRange: cue.timelineRange,
    })),
  });
  return {
    review,
    envelope: {
      projectId: request.projectId,
      sequenceId: request.sequenceId,
      baseProjectRevision: review.baseProjectRevision,
      preconditions,
      operation,
    },
  };
}

function normalizePreconditions(value: unknown): CaptionPrecondition[] {
  if (!Array.isArray(value) || value.length < 3 || value.length > 260) {
    throw new Error("Caption preconditions are invalid");
  }
  const seen = new Set<string>();
  return value.map((entry) => {
    const condition = asRecord(entry);
    assertExactKeys(condition, ["kind", "id", "revision"], "Caption precondition");
    const kind = condition.kind;
    if (
      kind !== "asset" &&
      kind !== "narrative-node" &&
      kind !== "clip" &&
      kind !== "track" &&
      kind !== "transcript-correction"
    ) {
      throw new Error("Caption precondition kind is invalid");
    }
    const normalized: CaptionPrecondition = {
      kind,
      id: durableID(condition.id),
      revision: revisionString(condition.revision),
    };
    const key = `${kind}\u0000${normalized.id}`;
    if (seen.has(key)) throw new Error("Caption precondition is duplicated");
    seen.add(key);
    return normalized;
  });
}

function requirePrecondition(
  values: readonly CaptionPrecondition[],
  kind: CaptionPrecondition["kind"],
  id: DurableID,
  revision: RevisionString,
) {
  if (!values.some((value) => value.kind === kind && value.id === id && value.revision === revision)) {
    throw new Error(`Caption ${kind} revision does not match its request`);
  }
}

function normalizeCaptionOperation(value: unknown, request: ReturnType<typeof normalizePreviewInput>) {
  const operation = asRecord(value);
  assertExactKeys(
    operation,
    ["type", "narrativeNode", "clip", "trackId", "captionPolicy", "derivedCaptions"],
    "Caption operation",
  );
  if (operation.type !== "derive-captions") throw new Error("Caption operation is invalid");
  if (exactReference(operation.narrativeNode, "Caption SourceExcerpt") !== request.sourceExcerptId) {
    throw new Error("Caption operation does not match its SourceExcerpt request");
  }
  if (exactReference(operation.clip, "Caption Clip") !== request.clipId) {
    throw new Error("Caption operation does not match its Clip request");
  }
  if (durableID(operation.trackId) !== request.trackId) {
    throw new Error("Caption operation does not match its Track request");
  }
  const captionPolicy = normalizeCaptionDerivationPolicy(operation.captionPolicy);
  if (
    !Array.isArray(operation.derivedCaptions) ||
    operation.derivedCaptions.length < 1 ||
    operation.derivedCaptions.length > 128
  ) {
    throw new Error("Caption derived cues are invalid");
  }
  const derivedCaptions = operation.derivedCaptions.map((entry, index) => {
    const cue = asRecord(entry);
    assertExactKeys(cue, ["alignmentAs", "captionAs", "sourceRange", "text", "timelineRange"], "Caption derived cue");
    const suffix = String(index + 1).padStart(3, "0");
    if (
      cue.captionAs !== `${request.localPrefix}_caption_${suffix}` ||
      cue.alignmentAs !== `${request.localPrefix}_alignment_${suffix}` ||
      typeof cue.text !== "string" ||
      new TextEncoder().encode(cue.text).length < 1 ||
      new TextEncoder().encode(cue.text).length > 262_144
    ) {
      throw new Error("Caption derived cue does not match its request");
    }
    return {
      captionAs: cue.captionAs,
      alignmentAs: cue.alignmentAs,
      sourceRange: normalizeTimeRange(cue.sourceRange),
      timelineRange: normalizeTimeRange(cue.timelineRange),
      text: cue.text,
    };
  });
  assertOrderedNonoverlapping(
    derivedCaptions.map((cue) => cue.sourceRange),
    "source",
  );
  assertOrderedNonoverlapping(
    derivedCaptions.map((cue) => cue.timelineRange),
    "timeline",
  );
  return {
    type: "derive-captions" as const,
    narrativeNode: { id: request.sourceExcerptId },
    clip: { id: request.clipId },
    trackId: request.trackId,
    captionPolicy,
    derivedCaptions,
  };
}

function exactReference(value: unknown, label: string): DurableID {
  const reference = asRecord(value);
  assertExactKeys(reference, ["id"], label);
  return durableID(reference.id);
}

function assertOrderedNonoverlapping(values: readonly TimeRange[], label: string): void {
  for (let index = 1; index < values.length; index += 1) {
    const previous = values[index - 1];
    const current = values[index];
    if (!previous || !current || compareRational(current.start, rangeEnd(previous)) < 0) {
      throw new Error(`Caption ${label} ranges are not ordered and non-overlapping`);
    }
  }
}

function rangeEnd(range: TimeRange) {
  const denominator = BigInt(range.start.scale) * BigInt(range.duration.scale);
  const numerator =
    BigInt(range.start.value) * BigInt(range.duration.scale) + BigInt(range.duration.value) * BigInt(range.start.scale);
  return { numerator, denominator };
}

function compareRational(
  left: Readonly<{ value: string; scale: number }>,
  right: Readonly<{ numerator: bigint; denominator: bigint }>,
) {
  const leftNumerator = BigInt(left.value) * right.denominator;
  const rightNumerator = right.numerator * BigInt(left.scale);
  return leftNumerator < rightNumerator ? -1 : leftNumerator > rightNumerator ? 1 : 0;
}

function assertExactKeys(value: Record<string, unknown>, keys: readonly string[], label: string) {
  const actual = Object.keys(value).sort();
  const expected = [...keys].sort();
  if (actual.length !== expected.length || actual.some((key, index) => key !== expected[index])) {
    throw new Error(`${label} has unexpected fields`);
  }
}
