import {
  type Caption,
  type CreatorEditCommit,
  CreatorEditError,
  type CreatorManualCaptionPort,
  type CreatorManualCaptionReview,
  cursorString,
  digestString,
  durableID,
  int64String,
  revisionString,
  type Track,
} from "@open-cut/contracts";
import { describe, expect, it, vi } from "vitest";

import type { TimelinePlayheadAuthority } from "../../src/lib/creator-timeline-controller.js";
import { ManualCaptionController } from "../../src/lib/manual-caption-controller.js";

const ids = {
  project: durableID("018f0a60-7b80-7a01-8000-000000000b01"),
  sequence: durableID("018f0a60-7b80-7a01-8000-000000000b02"),
  track: durableID("018f0a60-7b80-7a01-8000-000000000b03"),
  caption: durableID("018f0a60-7b80-7a01-8000-000000000b04"),
  proposal: durableID("018f0a60-7b80-7a01-8000-000000000b05"),
  transaction: durableID("018f0a60-7b80-7a01-8000-000000000b06"),
} as const;

describe("ManualCaptionController", () => {
  it("creates only from explicit Viewer In/Out marks without a hidden duration", async () => {
    const port = captionPort();
    const playhead = new FakePlayhead(2);
    const controller = new ManualCaptionController(port, playhead);
    controller.setProjection(projection([]));
    controller.beginCreate();
    controller.setText("Manual title");

    expect(() => controller.checkpoint()).toThrow("explicit In and Out");
    controller.captureIn();
    playhead.setPlayhead(time(5));
    controller.captureOut();
    const committed = await controller.checkpoint();

    expect(port.preview).toHaveBeenCalledWith({
      projectId: ids.project,
      sequenceId: ids.sequence,
      kind: "create",
      trackId: ids.track,
      trackRevision: "4",
      range: range(2, 3),
      language: "und",
      text: "Manual title",
    });
    expect(committed?.transactionId).toBe(ids.transaction);
    expect(controller.getSnapshot()).toMatchObject({ phase: "committed", draft: undefined });
  });

  it("requires an explicit destructive Alignment choice for content edits and retries one apply identically", async () => {
    const port = captionPort();
    vi.mocked(port.apply).mockRejectedValueOnce(new CreatorEditError("failed", 503)).mockResolvedValueOnce(receipt());
    const controller = new ManualCaptionController(port, new FakePlayhead(0));
    controller.setProjection(projection([caption()]));
    controller.selectCaption(ids.caption);
    controller.setText("Creator-polished wording");

    expect(controller.getSnapshot().draft?.alignmentHandling).toBeUndefined();
    expect(() => controller.chooseAlignmentHandling("preserve-if-provable")).toThrow("mark-stale or unbind");
    controller.chooseAlignmentHandling("mark-stale");
    expect(await controller.checkpoint()).toBeUndefined();
    expect(controller.getSnapshot()).toMatchObject({ phase: "error", canRetryIdenticalApply: true });
    const firstReview = vi.mocked(port.apply).mock.calls[0]?.[0];
    const firstInput = vi.mocked(port.apply).mock.calls[0]?.[1];

    expect((await controller.retryIdenticalApply())?.transactionId).toBe(ids.transaction);
    expect(vi.mocked(port.apply).mock.calls[1]?.[0]).toBe(firstReview);
    expect(vi.mocked(port.apply).mock.calls[1]?.[1]).toBe(firstInput);
    expect(port.preview).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: "update",
        captionId: ids.caption,
        captionRevision: "3",
        text: "Creator-polished wording",
        alignmentHandling: "mark-stale",
      }),
    );
  });

  it("preserves a dirty local value across revision conflict and adopts current revisions only after explicit refresh", () => {
    const controller = new ManualCaptionController(captionPort(), new FakePlayhead(0));
    controller.setProjection(projection([caption()]));
    controller.selectCaption(ids.caption);
    controller.setText("Unsaved local wording");

    const changed = { ...caption(), revision: revisionString("4"), text: "Other window" };
    controller.setProjection(projection([changed]));
    expect(controller.getSnapshot()).toMatchObject({
      phase: "conflict",
      draft: { text: "Unsaved local wording", dirty: true },
    });

    controller.prepareRefreshForRetry();
    controller.setProjection(projection([changed]));
    expect(controller.getSnapshot()).toMatchObject({
      phase: "drafting",
      draft: { text: "Unsaved local wording", dirty: true },
    });
  });
});

function captionPort(): CreatorManualCaptionPort {
  return {
    preview: vi.fn(async (input) => review(input.kind)),
    apply: vi.fn(async () => receipt()),
  };
}

function projection(captions: readonly Caption[]) {
  return { projectId: ids.project, sequenceId: ids.sequence, captions, tracks: [track()] };
}

function caption(): Caption {
  return {
    id: ids.caption,
    revision: revisionString("3"),
    sequenceId: ids.sequence,
    trackId: ids.track,
    range: range(2, 3),
    language: "en",
    text: "Derived wording",
    provenance: {
      kind: "transcript-derivation",
      derivation: {
        sourceExcerptId: ids.caption,
        sourceExcerptRevision: revisionString("1"),
        assetId: ids.caption,
        acceptedFingerprint: digestString(`sha256:${"a".repeat(64)}`),
        transcriptArtifactId: ids.caption,
        sourceStreamId: ids.caption,
        segmentIds: [ids.caption],
        correctionRevisions: [],
        clipId: ids.caption,
        clipRevision: revisionString("1"),
        clipSourceRange: range(0, 3),
        clipTimelineRange: range(2, 3),
        evidenceSourceRange: range(0, 3),
        policy: {
          id: "readable-captions-v1",
          maximumLines: 2,
          maximumLineGraphemes: 42,
          minimumDuration: time(1),
          maximumDuration: time(6),
          maximumGap: { value: int64String("3"), scale: 4 },
          maximumReadingRate: 20,
          boundaryPolicy: "terminal-punctuation-v1",
          timingPolicy: "forward-pad-no-overlap-v1",
          unicodeSegmentationId: "unicode-egc-15.0.0-uniseg-v0.4.7",
        },
        derivedRange: range(2, 3),
        derivedLanguage: "en",
        derivedText: "Derived wording",
      },
    },
    provenanceStatus: { content: "exact", evidence: "exact" },
    tombstoned: false,
  };
}

function track(): Track {
  return { id: ids.track, revision: revisionString("4"), type: "caption", label: "Captions" };
}

function review(kind: "create" | "update" | "remove"): CreatorManualCaptionReview {
  return {
    projectId: ids.project,
    sequenceId: ids.sequence,
    baseProjectRevision: revisionString("8"),
    activityCursor: cursorString("12"),
    outputDigest: digestString(`sha256:${"b".repeat(64)}`),
    kind,
    subject: {
      ...(kind === "create" ? {} : { captionId: ids.caption }),
      trackId: ids.track,
      range: range(2, 3),
      language: "en",
      text: "Derived wording",
      provenance: kind === "create" ? "manual" : "transcript-derivation",
    },
    alignmentEffects: [],
    preconditionCount: 3,
  };
}

function receipt(): CreatorEditCommit {
  return {
    proposalId: ids.proposal,
    transactionId: ids.transaction,
    committedProjectRevision: revisionString("9"),
    activityCursor: cursorString("13"),
    changes: [
      { kind: "caption", id: ids.caption, revision: revisionString("4"), tombstoned: false },
      { kind: "track", id: ids.track, revision: revisionString("5"), tombstoned: false },
    ],
    allocation: [],
    replayed: false,
  };
}

function range(start: number, duration: number) {
  return { start: time(start), duration: time(duration) };
}

function time(value: number) {
  return { value: int64String(String(value)), scale: 1 };
}

class FakePlayhead implements TimelinePlayheadAuthority {
  value;

  constructor(value: number) {
    this.value = time(value);
  }

  getSnapshot = () => ({ playhead: this.value });

  setPlayhead(value: { value: string; scale: number }): void {
    this.value = { value: int64String(value.value), scale: value.scale };
  }
}
