import { previewCreatorCaptionGesture } from "@open-cut/openapi/creator";

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
  type DigestString,
  type DurableID,
  digestString,
  durableID,
  type RevisionString,
  revisionString,
} from "./exact.js";

export type CreatorCaptionAlignmentHandling = "preserve-if-provable" | "mark-stale" | "unbind";

type ManualCaptionGestureCommon = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  trackId: DurableID;
  trackRevision: RevisionString;
}>;

export type CreatorManualCaptionGestureInput =
  | (ManualCaptionGestureCommon &
      Readonly<{
        kind: "create";
        range: TimeRange;
        language: string;
        text: string;
      }>)
  | (ManualCaptionGestureCommon &
      Readonly<{
        kind: "update";
        captionId: DurableID;
        captionRevision: RevisionString;
        range: TimeRange;
        language: string;
        text: string;
        alignmentHandling: CreatorCaptionAlignmentHandling;
      }>)
  | (ManualCaptionGestureCommon &
      Readonly<{
        kind: "remove";
        captionId: DurableID;
        captionRevision: RevisionString;
        alignmentHandling: "mark-stale" | "unbind";
      }>);

export type CreatorManualCaptionSubject = Readonly<{
  captionId?: DurableID;
  trackId: DurableID;
  range: TimeRange;
  language: string;
  text: string;
  provenance: "manual" | "transcript-derivation";
}>;

export type CreatorManualCaptionAlignmentEffect = Readonly<{
  alignmentId: DurableID;
  revision: RevisionString;
  handling: CreatorCaptionAlignmentHandling;
  targetCount: number;
}>;

export type CreatorManualCaptionReview = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  baseProjectRevision: RevisionString;
  activityCursor: CursorString;
  outputDigest: DigestString;
  kind: CreatorManualCaptionGestureInput["kind"];
  subject: CreatorManualCaptionSubject;
  alignmentEffects: readonly CreatorManualCaptionAlignmentEffect[];
  preconditionCount: number;
}>;

export type ApplyCreatorManualCaptionInput = Readonly<{ requestId: string; intent: string }>;

export interface CreatorManualCaptionPort {
  preview(input: CreatorManualCaptionGestureInput, signal?: AbortSignal): Promise<CreatorManualCaptionReview>;
  apply(
    review: CreatorManualCaptionReview,
    input: ApplyCreatorManualCaptionInput,
    signal?: AbortSignal,
  ): Promise<CreatorEditCommit>;
}

type ManualCaptionEnvelope = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  baseProjectRevision: CreatorWireEditBody["baseProjectRevision"];
  preconditions: CreatorWireEditBody["preconditions"];
  operations: CreatorWireEditBody["operations"];
}>;

type ManualCaptionPrecondition = Readonly<{
  kind: "sequence" | "track" | "caption" | "alignment";
  id: DurableID;
  revision: RevisionString;
}>;

export function createCreatorManualCaptionPort(): CreatorManualCaptionPort {
  const envelopes = new WeakMap<CreatorManualCaptionReview, ManualCaptionEnvelope>();
  return {
    preview: async (input, signal) => {
      const request = normalizeGestureInput(input);
      const response = await previewCreatorCaptionGesture(request.projectId, request.sequenceId, request.body, {
        signal,
      });
      if (response.status !== 200) throw creatorEditResponseError(response.status);
      const { review, envelope } = normalizeGestureReview(response.data, request);
      envelopes.set(review, envelope);
      return review;
    },
    apply: async (review, input, signal) => {
      const envelope = envelopes.get(review);
      if (!envelope) throw new Error("Creator manual Caption review is not owned by this Contracts session");
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

function normalizeGestureInput(input: CreatorManualCaptionGestureInput) {
  const common = {
    projectId: durableID(input.projectId),
    sequenceId: durableID(input.sequenceId),
    trackId: durableID(input.trackId),
    trackRevision: revisionString(input.trackRevision),
  };
  if (input.kind === "create") {
    const captionAs = `capm_${crypto.randomUUID().replaceAll("-", "")}`;
    const range = normalizeCaptionRange(input.range);
    const language = canonicalLanguage(input.language, "manual Caption");
    const value = captionText(input.text);
    return {
      ...common,
      kind: input.kind,
      captionAs,
      range,
      language,
      text: value,
      body: {
        kind: input.kind,
        captionAs,
        trackId: common.trackId,
        trackRevision: common.trackRevision,
        range,
        language,
        text: value,
      },
    } as const;
  }
  const captionId = durableID(input.captionId);
  const captionRevision = revisionString(input.captionRevision);
  const alignmentHandling = captionAlignmentHandling(input.alignmentHandling);
  if (input.kind === "update") {
    return {
      ...common,
      kind: input.kind,
      captionId,
      captionRevision,
      alignmentHandling,
      range: normalizeCaptionRange(input.range),
      language: canonicalLanguage(input.language, "manual Caption"),
      text: captionText(input.text),
      body: {
        kind: input.kind,
        captionId,
        captionRevision,
        trackId: common.trackId,
        trackRevision: common.trackRevision,
        range: normalizeCaptionRange(input.range),
        language: canonicalLanguage(input.language, "manual Caption"),
        text: captionText(input.text),
        alignmentHandling,
      },
    } as const;
  }
  if (alignmentHandling === "preserve-if-provable") {
    throw new Error("Removing a Caption cannot preserve its Alignment");
  }
  return {
    ...common,
    kind: input.kind,
    captionId,
    captionRevision,
    alignmentHandling,
    body: {
      kind: input.kind,
      captionId,
      captionRevision,
      trackId: common.trackId,
      trackRevision: common.trackRevision,
      alignmentHandling,
    },
  } as const;
}

function normalizeGestureReview(
  value: unknown,
  request: ReturnType<typeof normalizeGestureInput>,
): { review: CreatorManualCaptionReview; envelope: ManualCaptionEnvelope } {
  const payload = asRecord(value);
  assertExactKeys(
    payload,
    [
      "activityCursor",
      "alignmentEffects",
      "baseProjectRevision",
      "kind",
      "operations",
      "outputDigest",
      "preconditions",
      "subject",
    ],
    "manual Caption preview",
  );
  if (payload.kind !== request.kind) throw new Error("manual Caption preview does not match its request");
  const preconditions = normalizePreconditions(payload.preconditions);
  requirePrecondition(preconditions, "track", request.trackId, request.trackRevision);
  if (request.kind !== "create") {
    requirePrecondition(preconditions, "caption", request.captionId, request.captionRevision);
  }
  const subject = normalizeSubject(payload.subject, request);
  const effects = normalizeAlignmentEffects(payload.alignmentEffects, preconditions, request);
  const operations = normalizeOperations(payload.operations, request, effects, preconditions);
  const review: CreatorManualCaptionReview = Object.freeze({
    projectId: request.projectId,
    sequenceId: request.sequenceId,
    baseProjectRevision: revisionString(payload.baseProjectRevision),
    activityCursor: cursorString(payload.activityCursor),
    outputDigest: digestString(payload.outputDigest),
    kind: request.kind,
    subject,
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
    },
  };
}

function normalizeSubject(
  value: unknown,
  request: ReturnType<typeof normalizeGestureInput>,
): CreatorManualCaptionSubject {
  const subject = asRecord(value);
  assertExactKeys(
    subject,
    request.kind === "create"
      ? ["captionAs", "language", "provenance", "range", "text", "trackId"]
      : ["captionId", "language", "provenance", "range", "text", "trackId"],
    "manual Caption subject",
  );
  if (
    durableID(subject.trackId) !== request.trackId ||
    (subject.provenance !== "manual" && subject.provenance !== "transcript-derivation")
  ) {
    throw new Error("manual Caption subject is invalid");
  }
  const normalized = {
    ...(request.kind === "create" ? {} : { captionId: durableID(subject.captionId) }),
    trackId: request.trackId,
    range: normalizeCaptionRange(subject.range),
    language: canonicalLanguage(subject.language, "manual Caption subject"),
    text: captionText(subject.text),
    provenance: subject.provenance,
  } as const;
  if (request.kind === "create") {
    if (subject.captionAs !== request.body.captionAs || normalized.provenance !== "manual") {
      throw new Error("manual Caption subject does not match its create request");
    }
  } else if (normalized.captionId !== request.captionId) {
    throw new Error("manual Caption subject does not match its Caption request");
  }
  if (
    request.kind !== "remove" &&
    (!sameRange(normalized.range, request.range) ||
      normalized.language !== request.language ||
      normalized.text !== request.text)
  ) {
    throw new Error("manual Caption subject does not match its final value request");
  }
  return normalized;
}

function normalizePreconditions(value: unknown): ManualCaptionPrecondition[] {
  if (!Array.isArray(value) || value.length < 2 || value.length > 2048) {
    throw new Error("manual Caption preconditions are invalid");
  }
  const seen = new Set<string>();
  return value.map((entry) => {
    const condition = asRecord(entry);
    assertExactKeys(condition, ["kind", "id", "revision"], "manual Caption precondition");
    const kind = condition.kind;
    if (kind !== "sequence" && kind !== "track" && kind !== "caption" && kind !== "alignment") {
      throw new Error("manual Caption precondition kind is invalid");
    }
    const normalized: ManualCaptionPrecondition = {
      kind,
      id: durableID(condition.id),
      revision: revisionString(condition.revision),
    };
    const key = `${kind}\u0000${normalized.id}`;
    if (seen.has(key)) throw new Error("manual Caption precondition is duplicated");
    seen.add(key);
    return normalized;
  });
}

function normalizeAlignmentEffects(
  value: unknown,
  preconditions: readonly ManualCaptionPrecondition[],
  request: ReturnType<typeof normalizeGestureInput>,
): CreatorManualCaptionAlignmentEffect[] {
  if (!Array.isArray(value) || value.length > 511 || (request.kind === "create" && value.length !== 0)) {
    throw new Error("manual Caption Alignment effects are invalid");
  }
  let previous = "";
  return value.map((entry) => {
    const effect = asRecord(entry);
    assertExactKeys(effect, ["alignmentId", "handling", "revision", "targetCount"], "manual Caption effect");
    const alignmentId = durableID(effect.alignmentId);
    const revision = revisionString(effect.revision);
    const handling = captionAlignmentHandling(effect.handling);
    if (
      request.kind === "create" ||
      handling !== request.alignmentHandling ||
      !Number.isInteger(effect.targetCount) ||
      Number(effect.targetCount) < 1 ||
      Number(effect.targetCount) > 64 ||
      alignmentId <= previous
    ) {
      throw new Error("manual Caption Alignment effect does not match its request");
    }
    requirePrecondition(preconditions, "alignment", alignmentId, revision);
    previous = alignmentId;
    return { alignmentId, revision, handling, targetCount: Number(effect.targetCount) };
  });
}

function normalizeOperations(
  value: unknown,
  request: ReturnType<typeof normalizeGestureInput>,
  effects: readonly CreatorManualCaptionAlignmentEffect[],
  preconditions: readonly ManualCaptionPrecondition[],
): CreatorWireEditBody["operations"] {
  if (!Array.isArray(value) || value.length !== effects.length + 1) {
    throw new Error("manual Caption operations are invalid");
  }
  const primary = normalizePrimaryOperation(value[0], request);
  const alignments = effects.map((effect, index) =>
    normalizeAlignmentOperation(value[index + 1], effect, request, preconditions),
  );
  return [primary, ...alignments];
}

function normalizePrimaryOperation(value: unknown, request: ReturnType<typeof normalizeGestureInput>) {
  const operation = asRecord(value);
  if (request.kind === "create") {
    assertExactKeys(operation, ["type", "createAs", "trackId", "range", "language", "text"], "Caption create");
    if (
      operation.type !== "add-caption" ||
      operation.createAs !== request.body.captionAs ||
      durableID(operation.trackId) !== request.trackId ||
      !sameRequestedCaptionValue(operation, request)
    ) {
      throw new Error("Caption create does not match its request");
    }
    return {
      type: "add-caption" as const,
      createAs: request.body.captionAs,
      trackId: request.trackId,
      range: request.range,
      language: request.language,
      text: request.text,
    };
  }
  if (request.kind === "update") {
    assertExactKeys(operation, ["type", "captionId", "range", "language", "text"], "Caption update");
    if (
      operation.type !== "update-caption" ||
      durableID(operation.captionId) !== request.captionId ||
      !sameRequestedCaptionValue(operation, request)
    ) {
      throw new Error("Caption update does not match its request");
    }
    return {
      type: "update-caption" as const,
      captionId: request.captionId,
      range: request.range,
      language: request.language,
      text: request.text,
    };
  }
  assertExactKeys(operation, ["type", "captionId"], "Caption remove");
  if (operation.type !== "remove-caption" || durableID(operation.captionId) !== request.captionId) {
    throw new Error("Caption remove does not match its request");
  }
  return { type: "remove-caption" as const, captionId: request.captionId };
}

function sameRequestedCaptionValue(
  operation: Record<string, unknown>,
  request: Extract<ReturnType<typeof normalizeGestureInput>, { kind: "create" | "update" }>,
): boolean {
  return (
    sameRange(normalizeCaptionRange(operation.range), request.range) &&
    canonicalLanguage(operation.language, "manual Caption operation") === request.language &&
    captionText(operation.text) === request.text
  );
}

function normalizeAlignmentOperation(
  value: unknown,
  effect: CreatorManualCaptionAlignmentEffect,
  request: ReturnType<typeof normalizeGestureInput>,
  preconditions: readonly ManualCaptionPrecondition[],
) {
  const operation = asRecord(value);
  if (effect.handling === "mark-stale" || effect.handling === "unbind") {
    assertExactKeys(operation, ["type", "alignmentId"], "manual Caption Alignment operation");
    const expectedType = effect.handling === "mark-stale" ? "mark-alignment-stale" : "unbind-alignment";
    if (operation.type !== expectedType || durableID(operation.alignmentId) !== effect.alignmentId) {
      throw new Error("manual Caption Alignment operation does not match its effect");
    }
    return { type: expectedType, alignmentId: effect.alignmentId } as const;
  }
  assertExactKeys(operation, ["type", "alignmentId", "alignmentTargets"], "manual Caption Alignment remap");
  if (
    request.kind !== "update" ||
    operation.type !== "remap-alignment" ||
    durableID(operation.alignmentId) !== effect.alignmentId ||
    !Array.isArray(operation.alignmentTargets) ||
    operation.alignmentTargets.length !== effect.targetCount
  ) {
    throw new Error("manual Caption Alignment remap does not match its effect");
  }
  const targetIDs = new Set<string>();
  const targets = operation.alignmentTargets.map((entry) => {
    const target = asRecord(entry);
    assertExactKeys(target, ["type", "caption", "localRange"], "manual Caption Alignment target");
    const reference = asRecord(target.caption);
    assertExactKeys(reference, ["id"], "manual Caption Alignment reference");
    const captionId = durableID(reference.id);
    if (targetIDs.has(captionId)) throw new Error("manual Caption Alignment target is duplicated");
    if (!preconditions.some((condition) => condition.kind === "caption" && condition.id === captionId)) {
      throw new Error("manual Caption Alignment target is outside the exact precondition closure");
    }
    targetIDs.add(captionId);
    return {
      type: "caption" as const,
      caption: { id: captionId },
      localRange: normalizeCaptionRange(target.localRange),
    };
  });
  return { type: "remap-alignment" as const, alignmentId: effect.alignmentId, alignmentTargets: targets };
}

function requirePrecondition(
  values: readonly ManualCaptionPrecondition[],
  kind: ManualCaptionPrecondition["kind"],
  id: DurableID,
  revision: RevisionString,
) {
  if (!values.some((value) => value.kind === kind && value.id === id && value.revision === revision)) {
    throw new Error(`manual Caption ${kind} revision does not match its request`);
  }
}

function normalizeCaptionRange(value: unknown): TimeRange {
  const range = normalizeTimeRange(value);
  if (BigInt(range.start.value) < 0n) throw new Error("manual Caption range starts before zero");
  return range;
}

function captionText(value: unknown): string {
  if (typeof value !== "string") throw new Error("manual Caption text is invalid");
  const bytes = new TextEncoder().encode(value).length;
  if (bytes < 1 || bytes > 262_144) throw new Error("manual Caption text exceeds its budget");
  return value;
}

function captionAlignmentHandling(value: unknown): CreatorCaptionAlignmentHandling {
  if (value !== "preserve-if-provable" && value !== "mark-stale" && value !== "unbind") {
    throw new Error("manual Caption Alignment handling is invalid");
  }
  return value;
}

function sameRange(left: TimeRange, right: TimeRange): boolean {
  return (
    left.start.value === right.start.value &&
    left.start.scale === right.start.scale &&
    left.duration.value === right.duration.value &&
    left.duration.scale === right.duration.scale
  );
}

function assertExactKeys(value: Record<string, unknown>, keys: readonly string[], label: string) {
  const actual = Object.keys(value).sort();
  const expected = [...keys].sort();
  if (actual.length !== expected.length || actual.some((key, index) => key !== expected[index])) {
    throw new Error(`${label} has unexpected fields`);
  }
}
