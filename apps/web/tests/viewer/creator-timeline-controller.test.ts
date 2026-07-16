import {
  type Clip,
  type CreatorEditCommit,
  CreatorEditError,
  type CreatorTimelineGestureReview,
  type CreatorTimelinePort,
  cursorString,
  digestString,
  durableID,
  int64String,
  revisionString,
  type Track,
} from "@open-cut/contracts";
import { describe, expect, it, vi } from "vitest";

import {
  CreatorTimelineController,
  type TimelinePlayheadAuthority,
} from "../../src/lib/creator-timeline-controller.js";

const projectId = durableID("018f0a60-7b80-7a01-8000-000000000001");
const sequenceId = durableID("018f0a60-7b80-7a01-8000-000000000002");
const clipId = durableID("018f0a60-7b80-7a01-8000-000000000003");
const trackId = durableID("018f0a60-7b80-7a01-8000-000000000004");
const assetId = durableID("018f0a60-7b80-7a01-8000-000000000005");
const streamId = durableID("018f0a60-7b80-7a01-8000-000000000006");
const groupId = durableID("018f0a60-7b80-7a01-8000-000000000007");
const proposalId = durableID("018f0a60-7b80-7a01-8000-000000000008");
const transactionId = durableID("018f0a60-7b80-7a01-8000-000000000009");

describe("CreatorTimelineController", () => {
  it("requires an explicit linked scope and delegates exact split playhead authority", async () => {
    const port = timelinePort();
    const playhead = new FakePlayhead(5);
    const controller = new CreatorTimelineController(port, playhead);
    controller.setProjection({ projectId, sequenceId, clips: [clip(true)], tracks: [track()] });
    controller.selectClip(clipId);

    expect(controller.getSnapshot()).toMatchObject({ phase: "selected", selectedClip: { id: clipId } });
    expect(controller.getSnapshot().scope).toBeUndefined();
    expect(() => controller.splitAtPlayhead()).toThrow("not ready");

    controller.chooseScope("linked");
    controller.chooseAlignmentHandling("preserve-if-provable");
    const receipt = await controller.splitAtPlayhead();

    expect(port.preview).toHaveBeenCalledWith({
      projectId,
      sequenceId,
      kind: "split",
      clipId,
      clipRevision: "3",
      scope: "linked",
      alignmentHandling: "preserve-if-provable",
      splitAt: { value: "5", scale: 1 },
    });
    expect(port.apply).toHaveBeenCalledWith(
      review("split", "linked"),
      expect.objectContaining({ intent: "Split selected Timeline Clip" }),
    );
    expect(receipt?.transactionId).toBe(transactionId);
    expect(controller.getSnapshot()).toMatchObject({ phase: "committed", review: { kind: "split" } });

    controller.setPlayhead({ value: int64String("7"), scale: 1 });
    expect(playhead.value).toEqual({ value: "7", scale: 1 });
  });

  it("derives exact trim ranges from the Viewer playhead", async () => {
    const port = timelinePort();
    const controller = new CreatorTimelineController(port, new FakePlayhead(5));
    controller.setProjection({ projectId, sequenceId, clips: [clip(false)], tracks: [track()] });
    controller.selectClip(clipId);
    controller.chooseAlignmentHandling("preserve-if-provable");

    await controller.trimStartToPlayhead();

    expect(port.preview).toHaveBeenCalledWith({
      projectId,
      sequenceId,
      kind: "trim",
      clipId,
      clipRevision: "3",
      scope: "single",
      alignmentHandling: "preserve-if-provable",
      sourceRange: { start: { value: "13", scale: 1 }, duration: { value: "7", scale: 1 } },
      timelineRange: { start: { value: "5", scale: 1 }, duration: { value: "7", scale: 1 } },
    });
  });

  it("retries an ambiguous apply with the identical review and request identity", async () => {
    const port = timelinePort();
    vi.mocked(port.apply)
      .mockRejectedValueOnce(new CreatorEditError("failed", 503))
      .mockResolvedValueOnce(applyResult());
    const controller = new CreatorTimelineController(port, new FakePlayhead(6));
    controller.setProjection({ projectId, sequenceId, clips: [clip(false)], tracks: [track()] });
    controller.selectClip(clipId);
    controller.chooseAlignmentHandling("preserve-if-provable");

    expect(await controller.moveToPlayhead()).toBeUndefined();
    expect(controller.getSnapshot()).toMatchObject({ phase: "error", canRetryIdenticalApply: true });
    const firstReview = vi.mocked(port.apply).mock.calls[0]?.[0];
    const firstInput = vi.mocked(port.apply).mock.calls[0]?.[1];

    const committed = await controller.retryIdenticalApply();

    expect(committed?.transactionId).toBe(transactionId);
    expect(vi.mocked(port.apply).mock.calls[1]?.[0]).toBe(firstReview);
    expect(vi.mocked(port.apply).mock.calls[1]?.[1]).toBe(firstInput);
  });

  it("requires an explicit destructive Alignment policy and blocks conflict replay", async () => {
    const port = timelinePort();
    vi.mocked(port.apply).mockRejectedValueOnce(new CreatorEditError("conflict", 409));
    const controller = new CreatorTimelineController(port, new FakePlayhead(5));
    controller.setProjection({ projectId, sequenceId, clips: [clip(false)], tracks: [track()] });
    controller.selectClip(clipId);

    expect(() => controller.remove()).toThrow("mark-stale or unbind");
    controller.chooseAlignmentHandling("mark-stale");
    expect(await controller.remove()).toBeUndefined();
    expect(controller.getSnapshot()).toMatchObject({ phase: "conflict", canRetryIdenticalApply: false });
    expect(await controller.retryIdenticalApply()).toBeUndefined();
  });
});

function timelinePort(): CreatorTimelinePort {
  return {
    preview: vi.fn(async (input) => ({ status: "ready" as const, review: review(input.kind, input.scope) })),
    apply: vi.fn(async () => applyResult()),
  };
}

function clip(linked: boolean): Clip {
  return {
    id: clipId,
    revision: revisionString("3"),
    sequenceId,
    trackId,
    assetId,
    sourceStreamId: streamId,
    sourceRange: range(10, 10),
    timelineRange: range(2, 10),
    enabled: true,
    ...(linked ? { linkGroupId: groupId } : {}),
    tombstoned: false,
  };
}

function track(): Track {
  return { id: trackId, revision: revisionString("4"), type: "audio", label: "A1" };
}

function review(kind: "move" | "trim" | "split" | "remove", scope: "single" | "linked"): CreatorTimelineGestureReview {
  return {
    projectId,
    sequenceId,
    baseProjectRevision: revisionString("8"),
    activityCursor: cursorString("12"),
    outputDigest: digestString(`sha256:${"a".repeat(64)}`),
    kind,
    scope,
    seedClipId: clipId,
    affectedClipIds: [clipId],
    createdClipCount: kind === "split" ? 2 : 0,
    clipEffects: [
      {
        clipId,
        before: placement("3", 2),
        outcome: "updated",
        after: placement("4", 2),
      },
    ],
    alignmentEffects: [],
    preconditionCount: 3,
  };
}

function applyResult() {
  return { commit: receipt(), selectionHint: { clipId, revision: revisionString("4") } };
}

function placement(revision: string, start: number) {
  return {
    revision: revisionString(revision),
    trackId,
    sourceRange: range(10, 10),
    timelineRange: range(start, 10),
    linked: false,
  };
}

function receipt(): CreatorEditCommit {
  return {
    proposalId,
    transactionId,
    committedProjectRevision: revisionString("9"),
    activityCursor: cursorString("13"),
    changes: [{ kind: "clip", id: clipId, revision: revisionString("4"), tombstoned: false }],
    allocation: [],
    replayed: false,
  };
}

function range(start: number, duration: number) {
  return {
    start: { value: int64String(String(start)), scale: 1 },
    duration: { value: int64String(String(duration)), scale: 1 },
  };
}

class FakePlayhead implements TimelinePlayheadAuthority {
  value;

  constructor(value: number) {
    this.value = { value: int64String(String(value)), scale: 1 };
  }

  getSnapshot = () => ({ playhead: this.value });

  setPlayhead(value: { value: string; scale: number }): void {
    this.value = { value: int64String(value.value), scale: value.scale };
  }
}
