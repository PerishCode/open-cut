import { previewCreatorTimelineGesture } from "@open-cut/openapi/creator";

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
import {
  normalizeTimelineClipEffects,
  selectionHintForCommit,
  timelineBlockedReason,
  timelineBlockedRecoveries,
  timelinePostCommitSelection,
} from "./timeline-outcomes.js";

export type CreatorTimelineScope = "single" | "linked";
export type CreatorTimelineAlignmentHandling = "preserve-if-provable" | "mark-stale" | "unbind";

type TimelineGestureCommon = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  clipId: DurableID;
  clipRevision: RevisionString;
  scope: CreatorTimelineScope;
  alignmentHandling: CreatorTimelineAlignmentHandling;
}>;

export type CreatorTimelineGestureInput =
  | (TimelineGestureCommon &
      Readonly<{
        kind: "move";
        trackId: DurableID;
        trackRevision: RevisionString;
        timelineStart: RationalTime;
      }>)
  | (TimelineGestureCommon & Readonly<{ kind: "trim"; sourceRange: TimeRange; timelineRange: TimeRange }>)
  | (TimelineGestureCommon & Readonly<{ kind: "split"; splitAt: RationalTime }>)
  | (TimelineGestureCommon & Readonly<{ kind: "remove" }>);

export type CreatorTimelineAlignmentEffect = Readonly<{
  alignmentId: DurableID;
  revision: RevisionString;
  handling: CreatorTimelineAlignmentHandling;
  targetCount: number;
}>;

export type CreatorTimelineClipPlacement = Readonly<{
  revision: RevisionString;
  trackId: DurableID;
  sourceRange: TimeRange;
  timelineRange: TimeRange;
  linked: boolean;
}>;

export type CreatorTimelineClipEffect = Readonly<{
  clipId: DurableID;
  before: CreatorTimelineClipPlacement;
  outcome: "updated" | "split" | "removed";
  after?: CreatorTimelineClipPlacement;
  left?: CreatorTimelineClipPlacement;
  right?: CreatorTimelineClipPlacement;
}>;

export type CreatorTimelineGestureReview = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  baseProjectRevision: RevisionString;
  activityCursor: CursorString;
  outputDigest: DigestString;
  kind: CreatorTimelineGestureInput["kind"];
  scope: CreatorTimelineScope;
  seedClipId: DurableID;
  affectedClipIds: readonly DurableID[];
  createdClipCount: number;
  clipEffects: readonly CreatorTimelineClipEffect[];
  alignmentEffects: readonly CreatorTimelineAlignmentEffect[];
  preconditionCount: number;
}>;

export type CreatorTimelineBlockedReason =
  | "no-change"
  | "scope-unavailable"
  | "track-incompatible"
  | "range-invalid"
  | "track-collision"
  | "alignment-preserve-unprovable"
  | "closure-limit";

export type CreatorTimelineBlockedRecovery =
  | "choose-single"
  | "choose-compatible-track"
  | "change-target"
  | "mark-stale"
  | "unbind"
  | "reduce-scope";

export type CreatorTimelineGestureBlocked = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  baseProjectRevision: RevisionString;
  activityCursor: CursorString;
  kind: CreatorTimelineGestureInput["kind"];
  scope: CreatorTimelineScope;
  seedClipId: DurableID;
  reason: CreatorTimelineBlockedReason;
  subjectClipIds: readonly DurableID[];
  subjectAlignmentIds: readonly DurableID[];
  recoveries: readonly CreatorTimelineBlockedRecovery[];
}>;

export type CreatorTimelineGesturePlan =
  | Readonly<{ status: "ready"; review: CreatorTimelineGestureReview }>
  | Readonly<{ status: "blocked"; blocked: CreatorTimelineGestureBlocked }>;

export type CreatorTimelineSelectionHint = Readonly<{ clipId: DurableID; revision: RevisionString }>;

export type CreatorTimelineApplyResult = Readonly<{
  commit: CreatorEditCommit;
  selectionHint?: CreatorTimelineSelectionHint;
}>;

export type ApplyCreatorTimelineGestureInput = Readonly<{ requestId: string; intent: string }>;

export interface CreatorTimelinePort {
  preview(input: CreatorTimelineGestureInput, signal?: AbortSignal): Promise<CreatorTimelineGesturePlan>;
  apply(
    review: CreatorTimelineGestureReview,
    input: ApplyCreatorTimelineGestureInput,
    signal?: AbortSignal,
  ): Promise<CreatorTimelineApplyResult>;
}

type TimelineEnvelope = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  baseProjectRevision: CreatorWireEditBody["baseProjectRevision"];
  preconditions: CreatorWireEditBody["preconditions"];
  operations: CreatorWireEditBody["operations"];
  selection:
    | Readonly<{ kind: "same"; clipId: DurableID }>
    | Readonly<{ kind: "split-right"; local: string }>
    | Readonly<{ kind: "clear" }>;
}>;

type TimelinePrecondition = Readonly<{
  kind: "sequence" | "track" | "clip" | "link-group" | "alignment";
  id: DurableID;
  revision: RevisionString;
}>;

export function createCreatorTimelinePort(): CreatorTimelinePort {
  const envelopes = new WeakMap<CreatorTimelineGestureReview, TimelineEnvelope>();
  return {
    preview: async (input, signal) => {
      const normalized = normalizeGestureInput(input);
      const response = await previewCreatorTimelineGesture(
        normalized.projectId,
        normalized.sequenceId,
        normalized.body,
        { signal },
      );
      if (response.status !== 200) throw creatorEditResponseError(response.status);
      const plan = normalizeGesturePlan(response.data, normalized);
      if (plan.status === "ready") envelopes.set(plan.review, plan.envelope);
      return plan.status === "ready"
        ? Object.freeze({ status: "ready" as const, review: plan.review })
        : Object.freeze({ status: "blocked" as const, blocked: plan.blocked });
    },
    apply: async (review, input, signal) => {
      const envelope = envelopes.get(review);
      if (!envelope) throw new Error("Creator Timeline review is not owned by this Contracts session");
      validateCreatorRequestID(input.requestId);
      validateCreatorIntent(input.intent, false);
      const commit = await commitCreatorWireEdit(
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
      const selectionHint = selectionHintForCommit(envelope.selection, commit);
      return Object.freeze({ commit, ...(selectionHint === undefined ? {} : { selectionHint }) });
    },
  };
}

function normalizeGestureInput(input: CreatorTimelineGestureInput) {
  const common = {
    projectId: durableID(input.projectId),
    sequenceId: durableID(input.sequenceId),
    clipId: durableID(input.clipId),
    clipRevision: revisionString(input.clipRevision),
    scope: timelineScope(input.scope),
    alignmentHandling: alignmentHandling(input.alignmentHandling),
  };
  if (input.kind === "move") {
    const timelineStart = nonnegativeTime(input.timelineStart, "Timeline move");
    return {
      ...common,
      kind: input.kind,
      body: {
        kind: input.kind,
        clipId: common.clipId,
        clipRevision: common.clipRevision,
        scope: common.scope,
        alignmentHandling: common.alignmentHandling,
        trackId: durableID(input.trackId),
        trackRevision: revisionString(input.trackRevision),
        timelineStart,
      },
    } as const;
  }
  if (input.kind === "trim") {
    return {
      ...common,
      kind: input.kind,
      body: {
        kind: input.kind,
        clipId: common.clipId,
        clipRevision: common.clipRevision,
        scope: common.scope,
        alignmentHandling: common.alignmentHandling,
        sourceRange: normalizeTimeRange(input.sourceRange),
        timelineRange: nonnegativeRange(input.timelineRange, "Timeline trim placement"),
      },
    } as const;
  }
  if (input.kind === "split") {
    return {
      ...common,
      kind: input.kind,
      body: {
        kind: input.kind,
        clipId: common.clipId,
        clipRevision: common.clipRevision,
        scope: common.scope,
        alignmentHandling: common.alignmentHandling,
        splitAt: nonnegativeTime(input.splitAt, "Timeline split"),
        localPrefix: `tl_${crypto.randomUUID().replaceAll("-", "")}`,
      },
    } as const;
  }
  if (input.alignmentHandling === "preserve-if-provable") {
    throw new Error("Removing a Clip cannot preserve its Alignment");
  }
  return {
    ...common,
    kind: input.kind,
    body: {
      kind: input.kind,
      clipId: common.clipId,
      clipRevision: common.clipRevision,
      scope: common.scope,
      alignmentHandling: common.alignmentHandling,
    },
  } as const;
}

function normalizeGesturePlan(
  value: unknown,
  request: ReturnType<typeof normalizeGestureInput>,
):
  | { status: "ready"; review: CreatorTimelineGestureReview; envelope: TimelineEnvelope }
  | { status: "blocked"; blocked: CreatorTimelineGestureBlocked } {
  const payload = asRecord(value);
  if (payload.status === "ready") {
    assertExactKeys(payload, ["status", "ready"], "Timeline ready result");
    const normalized = normalizeGestureReview(payload.ready, request);
    return { status: "ready", ...normalized };
  }
  if (payload.status === "blocked") {
    assertExactKeys(payload, ["status", "blocked"], "Timeline blocked result");
    return { status: "blocked", blocked: normalizeGestureBlocked(payload.blocked, request) };
  }
  throw new Error("Timeline planner status is invalid");
}

function normalizeGestureReview(
  value: unknown,
  request: ReturnType<typeof normalizeGestureInput>,
): { review: CreatorTimelineGestureReview; envelope: TimelineEnvelope } {
  const payload = asRecord(value);
  assertExactKeys(
    payload,
    [
      "baseProjectRevision",
      "preconditions",
      "operations",
      "kind",
      "scope",
      "seedClipId",
      "affectedClipIds",
      "createdClipLocals",
      "clipEffects",
      "alignmentEffects",
      "outputDigest",
      "activityCursor",
    ],
    "Timeline ready preview",
  );
  const kind = gestureKind(payload.kind);
  const scope = timelineScope(payload.scope);
  const seedClipId = durableID(payload.seedClipId);
  if (kind !== request.kind || scope !== request.scope || seedClipId !== request.clipId) {
    throw new Error("Timeline preview does not match its request");
  }
  const preconditions = normalizePreconditions(payload.preconditions);
  requirePrecondition(preconditions, "clip", request.clipId, request.clipRevision);
  if (request.kind === "move") {
    requirePrecondition(preconditions, "track", request.body.trackId, request.body.trackRevision);
  }
  const affectedClipIds = distinctIDs(payload.affectedClipIds, "affected Clip", 64);
  if (!affectedClipIds.includes(seedClipId)) throw new Error("Timeline preview omits its seed Clip");
  const createdClipLocals = localIdentities(payload.createdClipLocals, 128);
  const effects = normalizeAlignmentEffects(payload.alignmentEffects, preconditions, request.alignmentHandling);
  const operations = normalizeOperations(payload.operations, request, affectedClipIds, createdClipLocals, effects);
  const clipEffects = normalizeTimelineClipEffects(payload.clipEffects, affectedClipIds, preconditions);
  const review: CreatorTimelineGestureReview = Object.freeze({
    projectId: request.projectId,
    sequenceId: request.sequenceId,
    baseProjectRevision: revisionString(payload.baseProjectRevision),
    activityCursor: cursorString(payload.activityCursor),
    outputDigest: digestString(payload.outputDigest),
    kind,
    scope,
    seedClipId,
    affectedClipIds,
    createdClipCount: createdClipLocals.length,
    clipEffects,
    alignmentEffects: effects,
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
      selection: timelinePostCommitSelection(
        request.kind,
        request.clipId,
        request.kind === "split" ? request.body.localPrefix : undefined,
        payload.operations,
      ),
    },
  };
}

function normalizeGestureBlocked(
  value: unknown,
  request: ReturnType<typeof normalizeGestureInput>,
): CreatorTimelineGestureBlocked {
  const payload = asRecord(value);
  assertExactKeys(
    payload,
    [
      "baseProjectRevision",
      "kind",
      "scope",
      "seedClipId",
      "reason",
      "subjectClipIds",
      "subjectAlignmentIds",
      "recoveries",
      "activityCursor",
    ],
    "Timeline blocked preview",
  );
  const kind = gestureKind(payload.kind);
  const scope = timelineScope(payload.scope);
  const seedClipId = durableID(payload.seedClipId);
  if (kind !== request.kind || scope !== request.scope || seedClipId !== request.clipId) {
    throw new Error("Timeline blocked preview does not match its request");
  }
  const reason = timelineBlockedReason(payload.reason);
  const subjectClipIds = distinctOptionalIDs(payload.subjectClipIds, "blocked Clip", 64);
  const subjectAlignmentIds = distinctOptionalIDs(payload.subjectAlignmentIds, "blocked Alignment", 512);
  const recoveries = timelineBlockedRecoveries(payload.recoveries, reason);
  return Object.freeze({
    projectId: request.projectId,
    sequenceId: request.sequenceId,
    baseProjectRevision: revisionString(payload.baseProjectRevision),
    activityCursor: cursorString(payload.activityCursor),
    kind,
    scope,
    seedClipId,
    reason,
    subjectClipIds,
    subjectAlignmentIds,
    recoveries,
  });
}

function normalizePreconditions(value: unknown): TimelinePrecondition[] {
  if (!Array.isArray(value) || value.length < 1 || value.length > 2048) {
    throw new Error("Timeline preconditions are invalid");
  }
  const seen = new Set<string>();
  return value.map((entry) => {
    const condition = asRecord(entry);
    assertExactKeys(condition, ["kind", "id", "revision"], "Timeline precondition");
    const kind = condition.kind;
    if (kind !== "sequence" && kind !== "track" && kind !== "clip" && kind !== "link-group" && kind !== "alignment") {
      throw new Error("Timeline precondition kind is invalid");
    }
    const normalized: TimelinePrecondition = {
      kind,
      id: durableID(condition.id),
      revision: revisionString(condition.revision),
    };
    const key = `${kind}\u0000${normalized.id}`;
    if (seen.has(key)) throw new Error("Timeline precondition is duplicated");
    seen.add(key);
    return normalized;
  });
}

function normalizeAlignmentEffects(
  value: unknown,
  preconditions: readonly TimelinePrecondition[],
  requestedHandling: CreatorTimelineAlignmentHandling,
): CreatorTimelineAlignmentEffect[] {
  if (!Array.isArray(value) || value.length > 2048) throw new Error("Timeline Alignment effects are invalid");
  const seen = new Set<string>();
  return value.map((entry) => {
    const effect = asRecord(entry);
    assertExactKeys(effect, ["alignmentId", "revision", "handling", "targetCount"], "Timeline Alignment effect");
    const alignmentId = durableID(effect.alignmentId);
    const revision = revisionString(effect.revision);
    const handling = alignmentHandling(effect.handling);
    if (handling !== requestedHandling || !boundedInteger(effect.targetCount, 0, 64) || seen.has(alignmentId)) {
      throw new Error("Timeline Alignment effect is invalid");
    }
    requirePrecondition(preconditions, "alignment", alignmentId, revision);
    seen.add(alignmentId);
    return { alignmentId, revision, handling, targetCount: effect.targetCount };
  });
}

function normalizeOperations(
  value: unknown,
  request: ReturnType<typeof normalizeGestureInput>,
  affectedClipIds: readonly DurableID[],
  createdLocals: readonly string[],
  effects: readonly CreatorTimelineAlignmentEffect[],
): CreatorWireEditBody["operations"] {
  if (!Array.isArray(value) || value.length !== effects.length + 1) {
    throw new Error("Timeline operation closure is invalid");
  }
  const mutation = normalizeMutation(value[0], request, affectedClipIds, createdLocals);
  const alignmentOperations = effects.map((effect, index) => normalizeAlignmentOperation(value[index + 1], effect));
  return [mutation, ...alignmentOperations] as CreatorWireEditBody["operations"];
}

function normalizeMutation(
  value: unknown,
  request: ReturnType<typeof normalizeGestureInput>,
  affectedClipIds: readonly DurableID[],
  createdLocals: readonly string[],
): CreatorWireEditBody["operations"][number] {
  const operation = asRecord(value);
  const clip = idReference(operation.clip, request.clipId, "Timeline mutation Clip");
  if (operation.scope !== request.scope) throw new Error("Timeline mutation scope is inconsistent");
  if (request.kind === "move") {
    assertExactKeys(operation, ["type", "clip", "scope", "trackId", "timelineStart"], "Timeline move");
    const timelineStart = nonnegativeTime(operation.timelineStart, "Timeline move");
    if (
      operation.type !== "move-clip" ||
      durableID(operation.trackId) !== request.body.trackId ||
      !sameTime(timelineStart, request.body.timelineStart)
    ) {
      throw new Error("Timeline move does not match its request");
    }
    if (createdLocals.length !== 0) throw new Error("Timeline move unexpectedly creates Clips");
    return { type: "move-clip", clip, scope: request.scope, trackId: request.body.trackId, timelineStart };
  }
  if (request.kind === "trim") {
    assertExactKeys(operation, ["type", "clip", "scope", "sourceRange", "timelineRange"], "Timeline trim");
    const sourceRange = normalizeTimeRange(operation.sourceRange);
    const timelineRange = nonnegativeRange(operation.timelineRange, "Timeline trim placement");
    if (
      operation.type !== "trim-clip" ||
      !sameRange(sourceRange, request.body.sourceRange) ||
      !sameRange(timelineRange, request.body.timelineRange) ||
      createdLocals.length !== 0
    ) {
      throw new Error("Timeline trim does not match its request");
    }
    return { type: "trim-clip", clip, scope: request.scope, sourceRange, timelineRange };
  }
  if (request.kind === "remove") {
    assertExactKeys(operation, ["type", "clip", "scope"], "Timeline remove");
    if (operation.type !== "remove-clip" || createdLocals.length !== 0) {
      throw new Error("Timeline remove does not match its request");
    }
    return { type: "remove-clip", clip, scope: request.scope };
  }
  assertExactKeys(
    operation,
    ["type", "clip", "scope", "splitAt", "splitOutputs", "leftLinkGroupAs", "rightLinkGroupAs"],
    "Timeline split",
  );
  const splitAt = nonnegativeTime(operation.splitAt, "Timeline split");
  if (operation.type !== "split-clip" || !sameTime(splitAt, request.body.splitAt)) {
    throw new Error("Timeline split does not match its request");
  }
  if (
    !Array.isArray(operation.splitOutputs) ||
    operation.splitOutputs.length < 1 ||
    operation.splitOutputs.length > 64
  ) {
    throw new Error("Timeline split outputs are invalid");
  }
  const outputLocals: string[] = [];
  const outputClips = new Set<string>();
  const splitOutputs = operation.splitOutputs.map((entry) => {
    const output = asRecord(entry);
    assertExactKeys(output, ["clip", "leftAs", "rightAs"], "Timeline split output");
    const outputClip = anyIDReference(output.clip, "Timeline split source Clip");
    if (!affectedClipIds.includes(outputClip.id) || outputClips.has(outputClip.id)) {
      throw new Error("Timeline split source Clip is invalid");
    }
    outputClips.add(outputClip.id);
    const leftAs = prefixedLocal(output.leftAs, request.body.localPrefix);
    const rightAs = prefixedLocal(output.rightAs, request.body.localPrefix);
    outputLocals.push(leftAs, rightAs);
    return { clip: outputClip, leftAs, rightAs };
  });
  if (!outputClips.has(request.clipId) || !sameStrings(outputLocals, createdLocals)) {
    throw new Error("Timeline split local allocation is inconsistent");
  }
  const groups = request.scope === "linked";
  const leftLinkGroupAs =
    operation.leftLinkGroupAs === undefined
      ? undefined
      : prefixedLocal(operation.leftLinkGroupAs, request.body.localPrefix);
  const rightLinkGroupAs =
    operation.rightLinkGroupAs === undefined
      ? undefined
      : prefixedLocal(operation.rightLinkGroupAs, request.body.localPrefix);
  if (groups !== Boolean(leftLinkGroupAs && rightLinkGroupAs)) {
    throw new Error("Timeline split LinkGroup allocation is inconsistent");
  }
  return {
    type: "split-clip",
    clip,
    scope: request.scope,
    splitAt,
    splitOutputs,
    ...(leftLinkGroupAs ? { leftLinkGroupAs } : {}),
    ...(rightLinkGroupAs ? { rightLinkGroupAs } : {}),
  };
}

function normalizeAlignmentOperation(
  value: unknown,
  effect: CreatorTimelineAlignmentEffect,
): CreatorWireEditBody["operations"][number] {
  const operation = asRecord(value);
  if (effect.handling === "mark-stale" || effect.handling === "unbind") {
    assertExactKeys(operation, ["type", "alignmentId"], "Timeline Alignment status operation");
    const type = effect.handling === "mark-stale" ? "mark-alignment-stale" : "unbind-alignment";
    if (operation.type !== type || durableID(operation.alignmentId) !== effect.alignmentId || effect.targetCount < 1) {
      throw new Error("Timeline Alignment status operation is inconsistent");
    }
    return { type, alignmentId: effect.alignmentId };
  }
  assertExactKeys(operation, ["type", "alignmentId", "alignmentTargets"], "Timeline Alignment remap");
  if (operation.type !== "remap-alignment" || durableID(operation.alignmentId) !== effect.alignmentId) {
    throw new Error("Timeline Alignment remap is inconsistent");
  }
  if (!Array.isArray(operation.alignmentTargets) || operation.alignmentTargets.length !== effect.targetCount) {
    throw new Error("Timeline Alignment remap target count is inconsistent");
  }
  const alignmentTargets = operation.alignmentTargets.map((entry) => {
    const target = asRecord(entry);
    assertExactKeys(target, ["type", "clip", "localRange"], "Timeline Alignment Clip target");
    if (target.type !== "clip") throw new Error("Timeline Alignment remap target is not a Clip");
    return {
      type: "clip" as const,
      clip: anyReference(target.clip, "Timeline Alignment Clip reference"),
      localRange: nonnegativeRange(target.localRange, "Timeline Alignment local range"),
    };
  });
  return { type: "remap-alignment", alignmentId: effect.alignmentId, alignmentTargets };
}

function requirePrecondition(
  values: readonly TimelinePrecondition[],
  kind: TimelinePrecondition["kind"],
  id: DurableID,
  revision: RevisionString,
) {
  if (!values.some((value) => value.kind === kind && value.id === id && value.revision === revision)) {
    throw new Error(`Timeline ${kind} revision does not match its request`);
  }
}

function idReference(value: unknown, expected: DurableID, label: string): { id: DurableID } {
  const reference = anyIDReference(value, label);
  if (reference.id !== expected) throw new Error(`${label} is inconsistent`);
  return reference;
}

function anyIDReference(value: unknown, label: string): { id: DurableID } {
  const reference = asRecord(value);
  assertExactKeys(reference, ["id"], label);
  return { id: durableID(reference.id) };
}

function anyReference(value: unknown, label: string): { id: DurableID } | { local: string } {
  const reference = asRecord(value);
  if (reference.id !== undefined && reference.local === undefined) {
    assertExactKeys(reference, ["id"], label);
    return { id: durableID(reference.id) };
  }
  if (reference.local !== undefined && reference.id === undefined) {
    assertExactKeys(reference, ["local"], label);
    return { local: localIdentity(reference.local) };
  }
  throw new Error(`${label} is invalid`);
}

function distinctIDs(value: unknown, label: string, maximum: number): DurableID[] {
  if (!Array.isArray(value) || value.length < 1 || value.length > maximum) throw new Error(`${label} list is invalid`);
  const result = value.map(durableID);
  if (new Set(result).size !== result.length) throw new Error(`${label} identity is duplicated`);
  return result;
}

function distinctOptionalIDs(value: unknown, label: string, maximum: number): DurableID[] {
  if (!Array.isArray(value) || value.length > maximum) throw new Error(`${label} list is invalid`);
  const result = value.map(durableID);
  if (new Set(result).size !== result.length) throw new Error(`${label} identity is duplicated`);
  return result;
}

function localIdentities(value: unknown, maximum: number): string[] {
  if (!Array.isArray(value) || value.length > maximum) throw new Error("Timeline local identities are invalid");
  const result = value.map(localIdentity);
  if (new Set(result).size !== result.length) throw new Error("Timeline local identity is duplicated");
  return result;
}

function localIdentity(value: unknown): string {
  if (typeof value !== "string" || !/^[a-z][a-z0-9_-]{0,63}$/.test(value)) {
    throw new Error("Timeline local identity is invalid");
  }
  return value;
}

function prefixedLocal(value: unknown, prefix: string): string {
  const local = localIdentity(value);
  if (!local.startsWith(`${prefix}_`)) throw new Error("Timeline local identity has the wrong prefix");
  return local;
}

function nonnegativeTime(value: unknown, label: string): RationalTime {
  const result = normalizeRational(value);
  if (BigInt(result.value) < 0n) throw new Error(`${label} time is negative`);
  return result;
}

function nonnegativeRange(value: unknown, label: string): TimeRange {
  const result = normalizeTimeRange(value);
  if (BigInt(result.start.value) < 0n) throw new Error(`${label} starts before zero`);
  return result;
}

function gestureKind(value: unknown): CreatorTimelineGestureInput["kind"] {
  if (value !== "move" && value !== "trim" && value !== "split" && value !== "remove") {
    throw new Error("Timeline gesture kind is invalid");
  }
  return value;
}

function timelineScope(value: unknown): CreatorTimelineScope {
  if (value !== "single" && value !== "linked") throw new Error("Timeline gesture scope is invalid");
  return value;
}

function alignmentHandling(value: unknown): CreatorTimelineAlignmentHandling {
  if (value !== "preserve-if-provable" && value !== "mark-stale" && value !== "unbind") {
    throw new Error("Timeline Alignment handling is invalid");
  }
  return value;
}

function assertExactKeys(value: Record<string, unknown>, keys: readonly string[], label: string) {
  const expected = new Set(keys);
  if (Object.keys(value).some((key) => !expected.has(key))) throw new Error(`${label} has unexpected fields`);
}

function boundedInteger(value: unknown, minimum: number, maximum: number): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= minimum && value <= maximum;
}

function sameTime(left: RationalTime, right: RationalTime): boolean {
  return left.value === right.value && left.scale === right.scale;
}

function sameRange(left: TimeRange, right: TimeRange): boolean {
  return sameTime(left.start, right.start) && sameTime(left.duration, right.duration);
}

function sameStrings(left: readonly string[], right: readonly string[]): boolean {
  return left.length === right.length && left.every((value, index) => value === right[index]);
}
