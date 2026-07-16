import { previewCreatorRoughCut } from "@open-cut/openapi/editing";

import {
  type CreatorEditCommit,
  type CreatorWireEditBody,
  commitCreatorWireEdit,
  creatorEditResponseError,
  validateCreatorIntent,
  validateCreatorRequestID,
} from "./creator-editing.js";
import { asRecord, normalizeRational, normalizeTimeRange, type TimeRange } from "./editing-exact.js";
import {
  type CursorString,
  cursorString,
  type DigestString,
  type DurableID,
  digestString,
  durableID,
  type RevisionString,
  revisionString,
} from "./exact.js";
import type { RationalTime } from "./projects.js";

export type CreatorRoughCutLaneInput = Readonly<{
  trackId: DurableID;
  trackRevision: RevisionString;
  sourceStreamId: DurableID;
}>;

export type CreatorRoughCutOccurrenceInput = Readonly<{
  sourceExcerptId: DurableID;
  sourceExcerptRevision: RevisionString;
  video?: CreatorRoughCutLaneInput;
  audio?: CreatorRoughCutLaneInput;
}>;

export type CreatorRoughCutPreviewInput = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  timelineStart: RationalTime;
  items: readonly CreatorRoughCutOccurrenceInput[];
}>;

export type CreatorRoughCutReviewLane = Readonly<{
  trackId: DurableID;
  sourceStreamId: DurableID;
  clipLocal: string;
}>;

export type CreatorRoughCutReviewItem = Readonly<{
  ordinal: number;
  sourceExcerptId: DurableID;
  sourceRange: TimeRange;
  timelineRange: TimeRange;
  video?: CreatorRoughCutReviewLane;
  audio?: CreatorRoughCutReviewLane;
  linkGroupLocal?: string;
  alignmentLocal: string;
}>;

export type CreatorRoughCutReview = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  baseProjectRevision: RevisionString;
  activityCursor: CursorString;
  outputDigest: DigestString;
  timelineStart: RationalTime;
  policy: Readonly<{
    id: "paper-edit-rough-cut-v1";
    ordering: "request-order";
    interExcerptGap: RationalTime;
    sourceHandles: "zero";
    rate: "1:1";
    overwrite: "forbidden";
    avGrouping: "one-link-group-per-two-lane-excerpt";
  }>;
  preconditionCount: number;
  items: readonly CreatorRoughCutReviewItem[];
}>;

export type ApplyCreatorRoughCutInput = Readonly<{ requestId: string; intent: string }>;

export interface CreatorRoughCutPort {
  preview(input: CreatorRoughCutPreviewInput, signal?: AbortSignal): Promise<CreatorRoughCutReview>;
  apply(
    review: CreatorRoughCutReview,
    input: ApplyCreatorRoughCutInput,
    signal?: AbortSignal,
  ): Promise<CreatorEditCommit>;
}

type RoughCutEnvelope = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  baseProjectRevision: CreatorWireEditBody["baseProjectRevision"];
  preconditions: CreatorWireEditBody["preconditions"];
  operation: CreatorWireEditBody["operations"][number];
}>;

type RoughCutPrecondition = Readonly<{
  kind: "asset" | "narrative-node" | "sequence" | "track" | "transcript-correction";
  id: DurableID;
  revision: RevisionString;
}>;

export function createCreatorRoughCutPort(): CreatorRoughCutPort {
  const envelopes = new WeakMap<CreatorRoughCutReview, RoughCutEnvelope>();
  return {
    preview: async (input, signal) => {
      const normalized = normalizePreviewInput(input);
      const projectId = durableID(input.projectId);
      const sequenceId = durableID(input.sequenceId);
      const response = await previewCreatorRoughCut(projectId, sequenceId, normalized, { signal });
      if (response.status !== 200) throw creatorEditResponseError(response.status);
      const { review, envelope } = normalizeReview(response.data, projectId, sequenceId, normalized);
      envelopes.set(review, envelope);
      return review;
    },
    apply: async (review, input, signal) => {
      const envelope = envelopes.get(review);
      if (!envelope) throw new Error("Creator rough-cut review is not owned by this Contracts session");
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

function normalizePreviewInput(input: CreatorRoughCutPreviewInput) {
  const timelineStart = normalizeRational(input.timelineStart);
  if (BigInt(timelineStart.value) < 0n || input.items.length < 1 || input.items.length > 128) {
    throw new Error("Creator rough-cut preview input is invalid");
  }
  return {
    timelineStart,
    localPrefix: `rough_${crypto.randomUUID().replaceAll("-", "")}`,
    items: input.items.map((item) => {
      const video = item.video ? normalizeInputLane(item.video) : undefined;
      const audio = item.audio ? normalizeInputLane(item.audio) : undefined;
      if (!video && !audio) throw new Error("Creator rough-cut occurrence has no bound lane");
      return {
        sourceExcerptId: durableID(item.sourceExcerptId),
        sourceExcerptRevision: revisionString(item.sourceExcerptRevision),
        ...(video ? { video } : {}),
        ...(audio ? { audio } : {}),
      };
    }),
  };
}

function normalizeInputLane(value: CreatorRoughCutLaneInput) {
  return {
    trackId: durableID(value.trackId),
    trackRevision: revisionString(value.trackRevision),
    sourceStreamId: durableID(value.sourceStreamId),
  };
}

function normalizeReview(
  value: unknown,
  projectId: DurableID,
  sequenceId: DurableID,
  request: ReturnType<typeof normalizePreviewInput>,
): { review: CreatorRoughCutReview; envelope: RoughCutEnvelope } {
  const payload = asRecord(value);
  const operation = normalizeRoughCutOperation(payload.operation);
  const outputDigest = digestString(payload.outputDigest);
  if (operation.roughCutOutputDigest !== outputDigest) throw new Error("rough-cut output digest is inconsistent");
  const preconditions = normalizePreconditions(payload.preconditions);
  validateRequestClosure(operation, preconditions, request);
  const review: CreatorRoughCutReview = Object.freeze({
    projectId,
    sequenceId,
    baseProjectRevision: revisionString(payload.baseProjectRevision),
    activityCursor: cursorString(payload.activityCursor),
    outputDigest,
    timelineStart: operation.roughCutTimelineStart,
    policy: operation.roughCutPolicy,
    preconditionCount: preconditions.length,
    items: operation.derivedRoughCut.map((output, index) => ({
      ordinal: index + 1,
      sourceExcerptId: output.sourceExcerptId,
      sourceRange: output.sourceRange,
      timelineRange: output.timelineRange,
      ...(output.video ? { video: reviewLane(output.video) } : {}),
      ...(output.audio ? { audio: reviewLane(output.audio) } : {}),
      ...(output.linkGroupAs ? { linkGroupLocal: output.linkGroupAs } : {}),
      alignmentLocal: output.alignmentAs,
    })),
  });
  return {
    review,
    envelope: {
      projectId,
      sequenceId,
      baseProjectRevision: review.baseProjectRevision,
      preconditions,
      operation,
    },
  };
}

function validateRequestClosure(
  operation: ReturnType<typeof normalizeRoughCutOperation>,
  preconditions: readonly RoughCutPrecondition[],
  request: ReturnType<typeof normalizePreviewInput>,
) {
  if (
    operation.roughCutLocalPrefix !== request.localPrefix ||
    operation.roughCutTimelineStart.value !== request.timelineStart.value ||
    operation.roughCutTimelineStart.scale !== request.timelineStart.scale ||
    operation.roughCutItems.length !== request.items.length
  ) {
    throw new Error("rough-cut preview does not match its request");
  }
  const revisions = new Map(
    preconditions.map((condition) => [`${condition.kind}\u0000${condition.id}`, condition.revision]),
  );
  request.items.forEach((requested, index) => {
    const actual = operation.roughCutItems[index];
    if (
      !actual ||
      actual.sourceExcerptId !== requested.sourceExcerptId ||
      !sameRequestedLane(actual.video, requested.video) ||
      !sameRequestedLane(actual.audio, requested.audio) ||
      revisions.get(`narrative-node\u0000${requested.sourceExcerptId}`) !== requested.sourceExcerptRevision
    ) {
      throw new Error("rough-cut preview item does not match its request");
    }
    for (const lane of [requested.video, requested.audio]) {
      if (lane && revisions.get(`track\u0000${lane.trackId}`) !== lane.trackRevision) {
        throw new Error("rough-cut preview Track revision does not match its request");
      }
    }
  });
}

function sameRequestedLane(
  actual: ReturnType<typeof normalizeOperationLane> | undefined,
  requested: ReturnType<typeof normalizeInputLane> | undefined,
): boolean {
  return (
    Boolean(actual) === Boolean(requested) &&
    (!actual ||
      !requested ||
      (actual.trackId === requested.trackId && actual.sourceStreamId === requested.sourceStreamId))
  );
}

function normalizePreconditions(value: unknown): readonly RoughCutPrecondition[] {
  if (!Array.isArray(value) || value.length > 2048) throw new Error("rough-cut preconditions are invalid");
  const seen = new Set<string>();
  return value.map((entry) => {
    const condition = asRecord(entry);
    const kind = condition.kind;
    if (
      kind !== "asset" &&
      kind !== "narrative-node" &&
      kind !== "sequence" &&
      kind !== "track" &&
      kind !== "transcript-correction"
    ) {
      throw new Error("rough-cut precondition kind is invalid");
    }
    const normalized: RoughCutPrecondition = {
      kind,
      id: durableID(condition.id),
      revision: revisionString(condition.revision),
    };
    const key = `${normalized.kind}\u0000${normalized.id}`;
    if (seen.has(key)) throw new Error("rough-cut precondition is duplicated");
    seen.add(key);
    return normalized;
  });
}

function normalizeRoughCutOperation(value: unknown) {
  const operation = asRecord(value);
  if (
    operation.type !== "derive-rough-cut" ||
    typeof operation.roughCutLocalPrefix !== "string" ||
    !/^[a-z][a-z0-9_-]{0,39}$/.test(operation.roughCutLocalPrefix) ||
    !Array.isArray(operation.roughCutItems) ||
    !Array.isArray(operation.derivedRoughCut) ||
    operation.roughCutItems.length < 1 ||
    operation.roughCutItems.length > 128 ||
    operation.derivedRoughCut.length !== operation.roughCutItems.length
  ) {
    throw new Error("rough-cut operation is invalid");
  }
  const policy = normalizePolicy(operation.roughCutPolicy);
  const timelineStart = normalizeRational(operation.roughCutTimelineStart);
  const items = operation.roughCutItems.map(normalizeOperationItem);
  const outputs = operation.derivedRoughCut.map((entry, index) => normalizeOutput(entry, items[index]));
  return {
    type: "derive-rough-cut" as const,
    roughCutPolicy: policy,
    roughCutTimelineStart: timelineStart,
    roughCutLocalPrefix: operation.roughCutLocalPrefix,
    roughCutItems: items,
    derivedRoughCut: outputs,
    roughCutOutputDigest: digestString(operation.roughCutOutputDigest),
  };
}

function normalizePolicy(value: unknown): CreatorRoughCutReview["policy"] {
  const policy = asRecord(value);
  const gap = normalizeRational(policy.interExcerptGap);
  if (
    policy.id !== "paper-edit-rough-cut-v1" ||
    policy.ordering !== "request-order" ||
    gap.value !== "0" ||
    gap.scale !== 1 ||
    policy.sourceHandles !== "zero" ||
    policy.rate !== "1:1" ||
    policy.overwrite !== "forbidden" ||
    policy.avGrouping !== "one-link-group-per-two-lane-excerpt"
  ) {
    throw new Error("rough-cut policy is invalid");
  }
  return {
    id: policy.id,
    ordering: policy.ordering,
    interExcerptGap: gap,
    sourceHandles: policy.sourceHandles,
    rate: policy.rate,
    overwrite: policy.overwrite,
    avGrouping: policy.avGrouping,
  };
}

function normalizeOperationItem(value: unknown) {
  const item = asRecord(value);
  const video = item.video === undefined ? undefined : normalizeOperationLane(item.video);
  const audio = item.audio === undefined ? undefined : normalizeOperationLane(item.audio);
  if (!video && !audio) throw new Error("rough-cut operation item has no lane");
  return {
    sourceExcerptId: durableID(item.sourceExcerptId),
    ...(video ? { video } : {}),
    ...(audio ? { audio } : {}),
  };
}

function normalizeOperationLane(value: unknown) {
  const lane = asRecord(value);
  return { trackId: durableID(lane.trackId), sourceStreamId: durableID(lane.sourceStreamId) };
}

function normalizeOutput(value: unknown, input: ReturnType<typeof normalizeOperationItem> | undefined) {
  if (!input) throw new Error("rough-cut output has no input");
  const output = asRecord(value);
  const sourceExcerptId = durableID(output.sourceExcerptId);
  const video = output.video === undefined ? undefined : normalizeOutputLane(output.video);
  const audio = output.audio === undefined ? undefined : normalizeOutputLane(output.audio);
  const hasTwoLanes = Boolean(video && audio);
  const linkGroupAs = output.linkGroupAs === undefined ? undefined : localIdentity(output.linkGroupAs);
  if (
    sourceExcerptId !== input.sourceExcerptId ||
    Boolean(video) !== Boolean(input.video) ||
    Boolean(audio) !== Boolean(input.audio) ||
    (video && input.video && !sameLane(video, input.video)) ||
    (audio && input.audio && !sameLane(audio, input.audio)) ||
    hasTwoLanes !== Boolean(linkGroupAs)
  ) {
    throw new Error("rough-cut output does not match its input");
  }
  return {
    sourceExcerptId,
    sourceRange: normalizeTimeRange(output.sourceRange),
    timelineRange: normalizeTimeRange(output.timelineRange),
    ...(video ? { video } : {}),
    ...(audio ? { audio } : {}),
    ...(linkGroupAs ? { linkGroupAs } : {}),
    alignmentAs: localIdentity(output.alignmentAs),
  };
}

function normalizeOutputLane(value: unknown) {
  const lane = asRecord(value);
  return {
    clipAs: localIdentity(lane.clipAs),
    trackId: durableID(lane.trackId),
    sourceStreamId: durableID(lane.sourceStreamId),
  };
}

function reviewLane(value: ReturnType<typeof normalizeOutputLane>): CreatorRoughCutReviewLane {
  return { clipLocal: value.clipAs, trackId: value.trackId, sourceStreamId: value.sourceStreamId };
}

function sameLane(
  left: ReturnType<typeof normalizeOutputLane>,
  right: ReturnType<typeof normalizeOperationLane>,
): boolean {
  return left.trackId === right.trackId && left.sourceStreamId === right.sourceStreamId;
}

function localIdentity(value: unknown): string {
  if (typeof value !== "string" || !/^[a-z][a-z0-9_-]{0,63}$/.test(value)) {
    throw new Error("rough-cut local identity is invalid");
  }
  return value;
}
