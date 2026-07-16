import { previewCreatorClipPlacement } from "@open-cut/openapi/creator";

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

export type CreatorClipPlacementLaneInput = Readonly<{
  trackId: DurableID;
  trackRevision: RevisionString;
  sourceStreamId: DurableID;
}>;

export type CreatorClipPlacementPreviewInput = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  assetId: DurableID;
  assetRevision: RevisionString;
  acceptedFingerprint: DigestString;
  sourceRange: TimeRange;
  timelineStart: RationalTime;
  video?: CreatorClipPlacementLaneInput;
  audio?: CreatorClipPlacementLaneInput;
}>;

export type CreatorClipPlacementLane = Readonly<{
  type: "video" | "audio";
  trackId: DurableID;
  sourceStreamId: DurableID;
}>;

export type CreatorClipPlacementReview = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  baseProjectRevision: RevisionString;
  activityCursor: CursorString;
  outputDigest: DigestString;
  assetId: DurableID;
  assetRevision: RevisionString;
  acceptedFingerprint: DigestString;
  sourceRange: TimeRange;
  timelineRange: TimeRange;
  lanes: readonly CreatorClipPlacementLane[];
  linked: boolean;
  preconditionCount: number;
}>;

export type ApplyCreatorClipPlacementInput = Readonly<{ requestId: string; intent: string }>;

export interface CreatorClipPlacementPort {
  preview(input: CreatorClipPlacementPreviewInput, signal?: AbortSignal): Promise<CreatorClipPlacementReview>;
  apply(
    review: CreatorClipPlacementReview,
    input: ApplyCreatorClipPlacementInput,
    signal?: AbortSignal,
  ): Promise<CreatorEditCommit>;
}

type PlacementEnvelope = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  baseProjectRevision: CreatorWireEditBody["baseProjectRevision"];
  preconditions: CreatorWireEditBody["preconditions"];
  operations: CreatorWireEditBody["operations"];
}>;

type PlacementPrecondition = Readonly<{
  kind: "asset" | "sequence" | "track";
  id: DurableID;
  revision: RevisionString;
}>;

export function createCreatorClipPlacementPort(): CreatorClipPlacementPort {
  const envelopes = new WeakMap<CreatorClipPlacementReview, PlacementEnvelope>();
  return {
    preview: async (input, signal) => {
      const request = normalizePlacementInput(input);
      const response = await previewCreatorClipPlacement(request.projectId, request.sequenceId, request.body, {
        signal,
      });
      if (response.status !== 200) throw creatorEditResponseError(response.status);
      const { review, envelope } = normalizePlacementReview(response.data, request);
      envelopes.set(review, envelope);
      return review;
    },
    apply: async (review, input, signal) => {
      const envelope = envelopes.get(review);
      if (!envelope) throw new Error("Creator Clip placement review is not owned by this Contracts session");
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
          operations: envelope.operations,
        },
        signal,
      );
    },
  };
}

function normalizePlacementInput(input: CreatorClipPlacementPreviewInput) {
  const sourceRange = normalizeTimeRange(input.sourceRange);
  const timelineStart = normalizeRational(input.timelineStart);
  if (BigInt(timelineStart.value) < 0n) throw new Error("Clip placement Timeline start is negative");
  const video = input.video ? normalizeInputLane(input.video) : undefined;
  const audio = input.audio ? normalizeInputLane(input.audio) : undefined;
  if (!video && !audio) throw new Error("Clip placement has no selected lane");
  if (video && audio && (video.trackId === audio.trackId || video.sourceStreamId === audio.sourceStreamId)) {
    throw new Error("Clip placement A/V lanes are not distinct");
  }
  const projectId = durableID(input.projectId);
  const sequenceId = durableID(input.sequenceId);
  const assetId = durableID(input.assetId);
  const assetRevision = revisionString(input.assetRevision);
  const acceptedFingerprint = digestString(input.acceptedFingerprint);
  const localPrefix = `place_${crypto.randomUUID().replaceAll("-", "")}`;
  return {
    projectId,
    sequenceId,
    assetId,
    assetRevision,
    acceptedFingerprint,
    sourceRange,
    timelineStart,
    localPrefix,
    video,
    audio,
    body: {
      assetId,
      assetRevision,
      acceptedFingerprint,
      sourceRange,
      timelineStart,
      localPrefix,
      ...(video ? { video } : {}),
      ...(audio ? { audio } : {}),
    },
  } as const;
}

function normalizeInputLane(value: CreatorClipPlacementLaneInput) {
  return {
    trackId: durableID(value.trackId),
    trackRevision: revisionString(value.trackRevision),
    sourceStreamId: durableID(value.sourceStreamId),
  };
}

function normalizePlacementReview(
  value: unknown,
  request: ReturnType<typeof normalizePlacementInput>,
): { review: CreatorClipPlacementReview; envelope: PlacementEnvelope } {
  const payload = asRecord(value);
  assertExactKeys(
    payload,
    [
      "acceptedFingerprint",
      "activityCursor",
      "assetId",
      "assetRevision",
      "baseProjectRevision",
      "lanes",
      "linked",
      "operations",
      "outputDigest",
      "preconditions",
      "sourceRange",
      "timelineRange",
    ],
    "Clip placement preview",
  );
  const assetId = durableID(payload.assetId);
  const assetRevision = revisionString(payload.assetRevision);
  const acceptedFingerprint = digestString(payload.acceptedFingerprint);
  const sourceRange = normalizeTimeRange(payload.sourceRange);
  const timelineRange = nonnegativeRange(payload.timelineRange, "Clip placement Timeline range");
  if (
    assetId !== request.assetId ||
    assetRevision !== request.assetRevision ||
    acceptedFingerprint !== request.acceptedFingerprint ||
    !sameRange(sourceRange, request.sourceRange) ||
    !sameRational(timelineRange.start, request.timelineStart) ||
    !sameRational(timelineRange.duration, request.sourceRange.duration)
  ) {
    throw new Error("Clip placement preview does not match its request");
  }
  const preconditions = normalizePreconditions(payload.preconditions);
  requirePrecondition(preconditions, "asset", assetId, assetRevision);
  requireSequencePrecondition(preconditions, request.sequenceId);
  const lanes = normalizeLanes(payload.lanes, request, preconditions);
  const linked = payload.linked === true;
  if (linked !== (lanes.length === 2)) throw new Error("Clip placement link state is inconsistent");
  const operations = normalizeOperations(payload.operations, request, lanes, sourceRange, timelineRange);
  const review: CreatorClipPlacementReview = Object.freeze({
    projectId: request.projectId,
    sequenceId: request.sequenceId,
    baseProjectRevision: revisionString(payload.baseProjectRevision),
    activityCursor: cursorString(payload.activityCursor),
    outputDigest: digestString(payload.outputDigest),
    assetId,
    assetRevision,
    acceptedFingerprint,
    sourceRange,
    timelineRange,
    lanes,
    linked,
    preconditionCount: preconditions.length,
  });
  return {
    review,
    envelope: {
      projectId: request.projectId,
      sequenceId: request.sequenceId,
      baseProjectRevision: review.baseProjectRevision,
      preconditions,
      operations,
    },
  };
}

function normalizePreconditions(value: unknown): PlacementPrecondition[] {
  if (!Array.isArray(value) || value.length < 3 || value.length > 4) {
    throw new Error("Clip placement preconditions are invalid");
  }
  const seen = new Set<string>();
  return value.map((entry) => {
    const condition = asRecord(entry);
    assertExactKeys(condition, ["id", "kind", "revision"], "Clip placement precondition");
    if (condition.kind !== "asset" && condition.kind !== "sequence" && condition.kind !== "track") {
      throw new Error("Clip placement precondition kind is invalid");
    }
    const result: PlacementPrecondition = {
      kind: condition.kind,
      id: durableID(condition.id),
      revision: revisionString(condition.revision),
    };
    const key = `${result.kind}\u0000${result.id}`;
    if (seen.has(key)) throw new Error("Clip placement precondition is duplicated");
    seen.add(key);
    return result;
  });
}

function normalizeLanes(
  value: unknown,
  request: ReturnType<typeof normalizePlacementInput>,
  preconditions: readonly PlacementPrecondition[],
): CreatorClipPlacementLane[] {
  const expected = [
    ...(request.video ? [{ type: "video" as const, ...request.video }] : []),
    ...(request.audio ? [{ type: "audio" as const, ...request.audio }] : []),
  ];
  if (!Array.isArray(value) || value.length !== expected.length) {
    throw new Error("Clip placement lanes are invalid");
  }
  return value.map((entry, index) => {
    const lane = asRecord(entry);
    assertExactKeys(lane, ["sourceStreamId", "trackId", "type"], "Clip placement lane");
    const requested = expected[index];
    if (!requested) throw new Error("Clip placement lane is unexpected");
    const result: CreatorClipPlacementLane = {
      type: lane.type === "video" || lane.type === "audio" ? lane.type : requested.type,
      trackId: durableID(lane.trackId),
      sourceStreamId: durableID(lane.sourceStreamId),
    };
    if (
      result.type !== requested.type ||
      result.trackId !== requested.trackId ||
      result.sourceStreamId !== requested.sourceStreamId
    ) {
      throw new Error("Clip placement lane does not match its request");
    }
    requirePrecondition(preconditions, "track", result.trackId, requested.trackRevision);
    return result;
  });
}

function normalizeOperations(
  value: unknown,
  request: ReturnType<typeof normalizePlacementInput>,
  lanes: readonly CreatorClipPlacementLane[],
  sourceRange: TimeRange,
  timelineRange: TimeRange,
): CreatorWireEditBody["operations"] {
  if (!Array.isArray(value) || value.length !== lanes.length) {
    throw new Error("Clip placement operations are invalid");
  }
  const linked = lanes.length === 2;
  const groupLocal = `${request.localPrefix}_group`;
  return value.map((entry, index) => {
    const operation = asRecord(entry);
    const lane = lanes[index];
    if (!lane) throw new Error("Clip placement operation has no lane");
    const baseKeys = [
      "assetId",
      "createAs",
      "enabled",
      "sourceRange",
      "sourceStreamId",
      "timelineRange",
      "trackId",
      "type",
    ];
    const expectedKeys = linked
      ? index === 0
        ? [...baseKeys, "createLinkGroupAs"]
        : [...baseKeys, "linkGroup"]
      : baseKeys;
    assertExactKeys(operation, expectedKeys, "Clip placement operation");
    const createAs = `${request.localPrefix}_${lane.type}`;
    if (
      operation.type !== "add-clip" ||
      operation.createAs !== createAs ||
      operation.enabled !== true ||
      durableID(operation.assetId) !== request.assetId ||
      durableID(operation.trackId) !== lane.trackId ||
      durableID(operation.sourceStreamId) !== lane.sourceStreamId ||
      !sameRange(normalizeTimeRange(operation.sourceRange), sourceRange) ||
      !sameRange(nonnegativeRange(operation.timelineRange, "Clip placement operation Timeline range"), timelineRange)
    ) {
      throw new Error("Clip placement operation does not match its request");
    }
    const normalized = {
      type: "add-clip" as const,
      createAs,
      trackId: lane.trackId,
      assetId: request.assetId,
      sourceStreamId: lane.sourceStreamId,
      sourceRange,
      timelineRange,
      enabled: true,
    };
    if (!linked) return normalized;
    if (index === 0) {
      if (operation.createLinkGroupAs !== groupLocal) throw new Error("Clip placement LinkGroup creation is invalid");
      return { ...normalized, createLinkGroupAs: groupLocal };
    }
    const reference = asRecord(operation.linkGroup);
    assertExactKeys(reference, ["local"], "Clip placement LinkGroup reference");
    if (reference.local !== groupLocal) throw new Error("Clip placement LinkGroup reference is invalid");
    return { ...normalized, linkGroup: { local: groupLocal } };
  });
}

function requirePrecondition(
  values: readonly PlacementPrecondition[],
  kind: PlacementPrecondition["kind"],
  id: DurableID,
  revision: RevisionString,
) {
  if (!values.some((value) => value.kind === kind && value.id === id && value.revision === revision)) {
    throw new Error(`Clip placement ${kind} revision does not match its request`);
  }
}

function requireSequencePrecondition(values: readonly PlacementPrecondition[], sequenceId: DurableID) {
  if (!values.some((value) => value.kind === "sequence" && value.id === sequenceId)) {
    throw new Error("Clip placement Sequence precondition is missing");
  }
}

function nonnegativeRange(value: unknown, label: string): TimeRange {
  const range = normalizeTimeRange(value);
  if (BigInt(range.start.value) < 0n) throw new Error(`${label} starts before zero`);
  return range;
}

function sameRange(left: TimeRange, right: TimeRange): boolean {
  return sameRational(left.start, right.start) && sameRational(left.duration, right.duration);
}

function sameRational(left: RationalTime, right: RationalTime): boolean {
  return left.value === right.value && left.scale === right.scale;
}

function assertExactKeys(value: Record<string, unknown>, keys: readonly string[], label: string) {
  const actual = Object.keys(value).sort();
  const expected = [...keys].sort();
  if (actual.length !== expected.length || actual.some((key, index) => key !== expected[index])) {
    throw new Error(`${label} has an unexpected shape`);
  }
}
