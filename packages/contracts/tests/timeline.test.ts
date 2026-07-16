import { afterEach, describe, expect, it, vi } from "vitest";

import { createContracts, durableID, int64String, revisionString } from "../src/index.js";
import { jsonResponse } from "./http-fixtures.js";
import { testIDs as ids } from "./test-identities.js";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("Creator Timeline Contracts", () => {
  it("keeps the exact gesture envelope opaque and byte-replays one direct Creator apply", async () => {
    const previewBodies: string[] = [];
    const applyBodies: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/timeline-gesture-preview")) {
          previewBodies.push(String(init?.body));
          return jsonResponse(timelineMovePreview());
        }
        if (url.endsWith("/edits")) {
          applyBodies.push(String(init?.body));
          if (applyBodies.length === 1) {
            return new Response(JSON.stringify({ title: "Unavailable", status: 503 }), {
              status: 503,
              headers: { "content-type": "application/problem+json" },
            });
          }
          return jsonResponse(commitReceipt());
        }
        throw new Error(`unexpected request ${url}`);
      }),
    );
    const port = createContracts().editing.timeline;
    const plan = await port.preview({
      projectId: durableID(ids.alpha),
      sequenceId: durableID(ids.alphaSequence),
      kind: "move",
      clipId: durableID(ids.clip),
      clipRevision: revisionString("3"),
      scope: "single",
      alignmentHandling: "preserve-if-provable",
      trackId: durableID(ids.audio),
      trackRevision: revisionString("4"),
      timelineStart: time(2, 1),
    });
    expect(plan.status).toBe("ready");
    if (plan.status !== "ready") throw new Error("expected ready Timeline plan");
    const review = plan.review;

    expect(JSON.parse(previewBodies[0] ?? "{}")).toEqual({
      kind: "move",
      clipId: ids.clip,
      clipRevision: "3",
      scope: "single",
      alignmentHandling: "preserve-if-provable",
      trackId: ids.audio,
      trackRevision: "4",
      timelineStart: { value: "2", scale: 1 },
    });
    expect(review).toEqual({
      projectId: ids.alpha,
      sequenceId: ids.alphaSequence,
      baseProjectRevision: "8",
      activityCursor: "12",
      outputDigest: `sha256:${"c".repeat(64)}`,
      kind: "move",
      scope: "single",
      seedClipId: ids.clip,
      affectedClipIds: [ids.clip],
      createdClipCount: 0,
      clipEffects: [
        {
          clipId: ids.clip,
          before: placement("3", 0),
          outcome: "updated",
          after: placement("4", 2),
        },
      ],
      alignmentEffects: [
        {
          alignmentId: ids.clipAlignment,
          revision: "2",
          handling: "preserve-if-provable",
          targetCount: 1,
        },
      ],
      preconditionCount: 4,
    });
    await expect(
      port.apply({ ...review }, { requestId: "ui:timeline:forged", intent: "Move selected Clip" }),
    ).rejects.toThrow("not owned by this Contracts session");

    const applyInput = { requestId: "ui:timeline:move-1", intent: "Move selected Clip" };
    await expect(port.apply(review, applyInput)).rejects.toMatchObject({ code: "failed", status: 503 });
    const applied = await port.apply(review, applyInput);

    expect(applyBodies).toHaveLength(2);
    expect(applyBodies[1]).toBe(applyBodies[0]);
    expect(JSON.parse(applyBodies[0] ?? "{}")).toEqual({
      requestId: applyInput.requestId,
      intent: applyInput.intent,
      baseProjectRevision: "8",
      preconditions: timelineMovePreview().ready.preconditions,
      operations: timelineMovePreview().ready.operations,
    });
    expect(applied).toMatchObject({
      commit: {
        transactionId: ids.transaction,
        committedProjectRevision: "9",
        changes: [
          { kind: "clip", id: ids.clip, revision: "4" },
          { kind: "alignment", id: ids.clipAlignment, revision: "3" },
        ],
      },
      selectionHint: { clipId: ids.clip, revision: "4" },
    });
  });

  it("rejects a planner response that does not close over the requested Clip revision", async () => {
    const preview = timelineMovePreview();
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse({
          ...preview,
          ready: {
            ...preview.ready,
            preconditions: preview.ready.preconditions.map((condition) =>
              condition.kind === "clip" ? { ...condition, revision: "2" } : condition,
            ),
          },
        }),
      ),
    );

    await expect(
      createContracts().editing.timeline.preview({
        projectId: durableID(ids.alpha),
        sequenceId: durableID(ids.alphaSequence),
        kind: "move",
        clipId: durableID(ids.clip),
        clipRevision: revisionString("3"),
        scope: "single",
        alignmentHandling: "preserve-if-provable",
        trackId: durableID(ids.audio),
        trackRevision: revisionString("4"),
        timelineStart: time(2, 1),
      }),
    ).rejects.toThrow("clip revision does not match");
  });

  it("returns a typed blocked outcome without manufacturing an apply envelope", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse({
          status: "blocked",
          blocked: {
            baseProjectRevision: "8",
            kind: "move",
            scope: "single",
            seedClipId: ids.clip,
            reason: "track-collision",
            subjectClipIds: [ids.clip],
            subjectAlignmentIds: [],
            recoveries: ["change-target"],
            activityCursor: "12",
          },
        }),
      ),
    );
    const port = createContracts().editing.timeline;
    const plan = await port.preview({
      projectId: durableID(ids.alpha),
      sequenceId: durableID(ids.alphaSequence),
      kind: "move",
      clipId: durableID(ids.clip),
      clipRevision: revisionString("3"),
      scope: "single",
      alignmentHandling: "preserve-if-provable",
      trackId: durableID(ids.audio),
      trackRevision: revisionString("4"),
      timelineStart: time(2, 1),
    });

    expect(plan).toEqual({
      status: "blocked",
      blocked: {
        projectId: ids.alpha,
        sequenceId: ids.alphaSequence,
        baseProjectRevision: "8",
        activityCursor: "12",
        kind: "move",
        scope: "single",
        seedClipId: ids.clip,
        reason: "track-collision",
        subjectClipIds: [ids.clip],
        subjectAlignmentIds: [],
        recoveries: ["change-target"],
      },
    });
  });
});

function timelineMovePreview() {
  const localRange = range(0, 1, 1, 1);
  return {
    status: "ready",
    ready: {
      baseProjectRevision: "8",
      preconditions: [
        { kind: "alignment", id: ids.clipAlignment, revision: "2" },
        { kind: "clip", id: ids.clip, revision: "3" },
        { kind: "sequence", id: ids.alphaSequence, revision: "5" },
        { kind: "track", id: ids.audio, revision: "4" },
      ],
      operations: [
        {
          type: "move-clip",
          clip: { id: ids.clip },
          scope: "single",
          trackId: ids.audio,
          timelineStart: time(2, 1),
        },
        {
          type: "remap-alignment",
          alignmentId: ids.clipAlignment,
          alignmentTargets: [{ type: "clip", clip: { id: ids.clip }, localRange }],
        },
      ],
      kind: "move",
      scope: "single",
      seedClipId: ids.clip,
      affectedClipIds: [ids.clip],
      createdClipLocals: [],
      clipEffects: [
        {
          clipId: ids.clip,
          before: placement("3", 0),
          outcome: "updated",
          after: placement("4", 2),
        },
      ],
      alignmentEffects: [
        {
          alignmentId: ids.clipAlignment,
          revision: "2",
          handling: "preserve-if-provable",
          targetCount: 1,
        },
      ],
      outputDigest: `sha256:${"c".repeat(64)}`,
      activityCursor: "12",
    },
  };
}

function placement(revision: string, start: number) {
  return {
    revision,
    trackId: ids.audio,
    sourceRange: range(0, 1, 1, 1),
    timelineRange: range(start, 1, 1, 1),
    linked: false,
  };
}

function commitReceipt() {
  return {
    proposal: {
      id: ids.proposal,
      projectId: ids.alpha,
      sequenceId: ids.alphaSequence,
      requestId: "ui:timeline:move-1",
      actor: { kind: "creator", creatorId: ids.creator },
      status: "applied",
      appliedTransactionId: ids.transaction,
      allocation: [],
    },
    transaction: {
      id: ids.transaction,
      proposalId: ids.proposal,
      projectId: ids.alpha,
      actor: { kind: "creator", creatorId: ids.creator },
      committedProjectRevision: "9",
      changes: [
        { kind: "clip", id: ids.clip, before: "3", after: "4" },
        { kind: "alignment", id: ids.clipAlignment, before: "2", after: "3" },
      ],
    },
    activityCursor: "13",
    replayed: false,
  };
}

function range(startValue: number, startScale: number, durationValue: number, durationScale: number) {
  return { start: time(startValue, startScale), duration: time(durationValue, durationScale) };
}

function time(value: number, scale: number) {
  return { value: int64String(String(value)), scale };
}
