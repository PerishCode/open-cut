import type { CreatorEditCommit } from "./creator-editing.js";
import { asRecord, normalizeTimeRange } from "./editing-exact.js";
import { type DurableID, durableID, type RevisionString, revisionString } from "./exact.js";
import type {
  CreatorTimelineBlockedReason,
  CreatorTimelineBlockedRecovery,
  CreatorTimelineClipEffect,
  CreatorTimelineClipPlacement,
  CreatorTimelineGestureInput,
  CreatorTimelineSelectionHint,
} from "./timeline.js";

export type TimelinePostCommitSelection =
  | Readonly<{ kind: "same"; clipId: DurableID }>
  | Readonly<{ kind: "split-right"; local: string }>
  | Readonly<{ kind: "clear" }>;

type ClipPrecondition = Readonly<{
  kind: string;
  id: DurableID;
  revision: RevisionString;
}>;

export function normalizeTimelineClipEffects(
  value: unknown,
  affectedClipIds: readonly DurableID[],
  preconditions: readonly ClipPrecondition[],
): CreatorTimelineClipEffect[] {
  if (!Array.isArray(value) || value.length !== affectedClipIds.length || value.length < 1 || value.length > 64) {
    throw new Error("Timeline Clip effects are invalid");
  }
  const seen = new Set<string>();
  const effects = value.map((entry) => {
    const effect = asRecord(entry);
    const clipId = durableID(effect.clipId);
    if (!affectedClipIds.includes(clipId) || seen.has(clipId))
      throw new Error("Timeline Clip effect identity is invalid");
    seen.add(clipId);
    const before = normalizeClipPlacement(effect.before, "before");
    const expected = preconditions.find((item) => item.kind === "clip" && item.id === clipId);
    if (!expected || expected.revision !== before.revision) {
      throw new Error("Timeline Clip effect does not match its precondition");
    }
    if (effect.outcome === "updated") {
      assertExactKeys(effect, ["clipId", "before", "outcome", "after"], "updated Timeline Clip effect");
      const after = normalizeClipPlacement(effect.after, "after");
      requireNextRevision(before.revision, after.revision);
      return Object.freeze({ clipId, before, outcome: "updated" as const, after });
    }
    if (effect.outcome === "split") {
      assertExactKeys(effect, ["clipId", "before", "outcome", "left", "right"], "split Timeline Clip effect");
      const left = normalizeClipPlacement(effect.left, "split left");
      const right = normalizeClipPlacement(effect.right, "split right");
      if (left.revision !== "1" || right.revision !== "1") {
        throw new Error("Timeline split output revision is invalid");
      }
      return Object.freeze({ clipId, before, outcome: "split" as const, left, right });
    }
    if (effect.outcome === "removed") {
      assertExactKeys(effect, ["clipId", "before", "outcome"], "removed Timeline Clip effect");
      return Object.freeze({ clipId, before, outcome: "removed" as const });
    }
    throw new Error("Timeline Clip effect outcome is invalid");
  });
  if (affectedClipIds.some((id) => !seen.has(id))) throw new Error("Timeline Clip effect closure is incomplete");
  return effects;
}

function normalizeClipPlacement(value: unknown, label: string): CreatorTimelineClipPlacement {
  const placement = asRecord(value);
  assertExactKeys(
    placement,
    ["revision", "trackId", "sourceRange", "timelineRange", "linked"],
    `${label} Timeline Clip placement`,
  );
  if (typeof placement.linked !== "boolean") throw new Error(`Timeline Clip ${label} linked state is invalid`);
  const timelineRange = normalizeTimeRange(placement.timelineRange);
  if (BigInt(timelineRange.start.value) < 0n) throw new Error(`Timeline Clip ${label} starts before zero`);
  return Object.freeze({
    revision: revisionString(placement.revision),
    trackId: durableID(placement.trackId),
    sourceRange: normalizeTimeRange(placement.sourceRange),
    timelineRange,
    linked: placement.linked,
  });
}

export function timelineBlockedReason(value: unknown): CreatorTimelineBlockedReason {
  if (
    value !== "no-change" &&
    value !== "scope-unavailable" &&
    value !== "track-incompatible" &&
    value !== "range-invalid" &&
    value !== "track-collision" &&
    value !== "alignment-preserve-unprovable" &&
    value !== "closure-limit"
  ) {
    throw new Error("Timeline blocked reason is invalid");
  }
  return value;
}

export function timelineBlockedRecoveries(
  value: unknown,
  reason: CreatorTimelineBlockedReason,
): CreatorTimelineBlockedRecovery[] {
  if (!Array.isArray(value) || value.length > 4) throw new Error("Timeline blocked recoveries are invalid");
  const expected = expectedRecoveries(reason);
  const recoveries = value.map(blockedRecovery);
  if (recoveries.length !== expected.length || recoveries.some((item, index) => item !== expected[index])) {
    throw new Error("Timeline blocked recoveries do not match their reason");
  }
  return recoveries;
}

function expectedRecoveries(reason: CreatorTimelineBlockedReason): CreatorTimelineBlockedRecovery[] {
  switch (reason) {
    case "no-change":
      return [];
    case "scope-unavailable":
      return ["choose-single"];
    case "track-incompatible":
      return ["choose-compatible-track"];
    case "range-invalid":
    case "track-collision":
      return ["change-target"];
    case "alignment-preserve-unprovable":
      return ["mark-stale", "unbind"];
    case "closure-limit":
      return ["reduce-scope"];
  }
}

function blockedRecovery(value: unknown): CreatorTimelineBlockedRecovery {
  if (
    value !== "choose-single" &&
    value !== "choose-compatible-track" &&
    value !== "change-target" &&
    value !== "mark-stale" &&
    value !== "unbind" &&
    value !== "reduce-scope"
  ) {
    throw new Error("Timeline blocked recovery is invalid");
  }
  return value;
}

export function timelinePostCommitSelection(
  kind: CreatorTimelineGestureInput["kind"],
  seedClipId: DurableID,
  localPrefix: string | undefined,
  operations: unknown,
): TimelinePostCommitSelection {
  if (kind === "move" || kind === "trim") return { kind: "same", clipId: seedClipId };
  if (kind === "remove") return { kind: "clear" };
  if (localPrefix === undefined || !Array.isArray(operations) || operations.length < 1) {
    throw new Error("Timeline split selection closure is invalid");
  }
  const mutation = asRecord(operations[0]);
  if (!Array.isArray(mutation.splitOutputs)) throw new Error("Timeline split selection outputs are invalid");
  for (const entry of mutation.splitOutputs) {
    const output = asRecord(entry);
    const clip = asRecord(output.clip);
    if (durableID(clip.id) === seedClipId) {
      const local = localIdentity(output.rightAs);
      if (!local.startsWith(`${localPrefix}_`)) throw new Error("Timeline split selection local is invalid");
      return { kind: "split-right", local };
    }
  }
  throw new Error("Timeline split selection omits its seed output");
}

export function selectionHintForCommit(
  selection: TimelinePostCommitSelection,
  commit: CreatorEditCommit,
): CreatorTimelineSelectionHint | undefined {
  if (selection.kind === "clear") return undefined;
  let clipId: DurableID;
  if (selection.kind === "same") {
    clipId = selection.clipId;
  } else {
    const allocation = commit.allocation.find((item) => item.kind === "clip" && item.local === selection.local);
    if (!allocation) throw new Error("Timeline commit omits the selected split output allocation");
    clipId = allocation.id;
  }
  const change = commit.changes.find((item) => item.kind === "clip" && item.id === clipId && !item.tombstoned);
  if (!change) throw new Error("Timeline commit omits the selected Clip revision");
  return Object.freeze({ clipId, revision: change.revision });
}

function requireNextRevision(before: RevisionString, after: RevisionString): void {
  if (BigInt(after) !== BigInt(before) + 1n) throw new Error("Timeline Clip effect revision is not contiguous");
}

function localIdentity(value: unknown): string {
  if (typeof value !== "string" || !/^[a-z][a-z0-9_-]{0,63}$/.test(value)) {
    throw new Error("Timeline local identity is invalid");
  }
  return value;
}

function assertExactKeys(value: Record<string, unknown>, keys: readonly string[], label: string): void {
  const expected = new Set(keys);
  if (Object.keys(value).some((key) => !expected.has(key))) throw new Error(`${label} has unexpected fields`);
}
