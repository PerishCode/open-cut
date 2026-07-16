import {
  type Alignment,
  type Clip,
  type CreatorCaptionPort,
  type CreatorCaptionReview,
  type CreatorEditCommit,
  CreatorEditError,
  cursorString,
  digestString,
  durableID,
  int64String,
  revisionString,
  type SourceExcerpt,
  type Track,
} from "@open-cut/contracts";
import { describe, expect, it, vi } from "vitest";

import { CreatorCaptionController, captionClipCandidates } from "../../src/lib/creator-caption-controller.js";

const ids = {
  project: durableID("018f0a60-7b80-7a01-8000-000000000901"),
  sequence: durableID("018f0a60-7b80-7a01-8000-000000000902"),
  document: durableID("018f0a60-7b80-7a01-8000-000000000903"),
  root: durableID("018f0a60-7b80-7a01-8000-000000000904"),
  excerpt: durableID("018f0a60-7b80-7a01-8000-000000000905"),
  asset: durableID("018f0a60-7b80-7a01-8000-000000000906"),
  transcript: durableID("018f0a60-7b80-7a01-8000-000000000907"),
  segment: durableID("018f0a60-7b80-7a01-8000-000000000908"),
  stream: durableID("018f0a60-7b80-7a01-8000-000000000909"),
  otherStream: durableID("018f0a60-7b80-7a01-8000-00000000090a"),
  alignedClip: durableID("018f0a60-7b80-7a01-8000-00000000090b"),
  streamClip: durableID("018f0a60-7b80-7a01-8000-00000000090c"),
  rangeClip: durableID("018f0a60-7b80-7a01-8000-00000000090d"),
  mediaTrack: durableID("018f0a60-7b80-7a01-8000-00000000090e"),
  captionTrack: durableID("018f0a60-7b80-7a01-8000-00000000090f"),
  captionTrack2: durableID("018f0a60-7b80-7a01-8000-000000000910"),
  alignment: durableID("018f0a60-7b80-7a01-8000-000000000911"),
  proposal: durableID("018f0a60-7b80-7a01-8000-000000000912"),
  transaction: durableID("018f0a60-7b80-7a01-8000-000000000913"),
} as const;

describe("CreatorCaptionController", () => {
  it("ranks explainable Clip recommendations but leaves every ambiguous choice unresolved", () => {
    const source = selection();
    const candidates = captionClipCandidates(
      source,
      [
        clip(ids.rangeClip, ids.otherStream, 4),
        clip(ids.streamClip, ids.stream, 2),
        clip(ids.alignedClip, ids.otherStream, 8),
      ],
      [alignment()],
    );

    expect(candidates.map((candidate) => [candidate.clip.id, candidate.recommendation])).toEqual([
      [ids.alignedClip, "exact-alignment"],
      [ids.streamClip, "source-stream"],
      [ids.rangeClip, "compatible-range"],
    ]);

    const controller = new CreatorCaptionController(captionPort());
    controller.setProjection({
      projectId: ids.project,
      sequenceId: ids.sequence,
      source,
      clips: candidates.map((candidate) => candidate.clip),
      alignments: [alignment()],
      tracks: [track(ids.captionTrack, "Captions"), track(ids.captionTrack2, "Translations")],
    });

    expect(controller.getSnapshot()).toMatchObject({ phase: "ready" });
    expect(controller.getSnapshot().selectedClip).toBeUndefined();
    expect(controller.getSnapshot().selectedTrack).toBeUndefined();
    controller.selectClip(ids.alignedClip);
    controller.selectTrack(ids.captionTrack);
    expect(controller.getSnapshot()).toMatchObject({
      selectedClip: { id: ids.alignedClip },
      selectedTrack: { id: ids.captionTrack },
    });
  });

  it("prefills unique authorities, reviews immutable cues, and retries one ambiguous apply identically", async () => {
    const port = captionPort();
    vi.mocked(port.apply).mockRejectedValueOnce(new CreatorEditError("failed", 503)).mockResolvedValueOnce(receipt());
    const controller = new CreatorCaptionController(port);
    controller.setProjection({
      projectId: ids.project,
      sequenceId: ids.sequence,
      source: selection(),
      clips: [clip(ids.streamClip, ids.stream, 2)],
      alignments: [],
      tracks: [track(ids.captionTrack, "Captions")],
    });

    expect(controller.getSnapshot()).toMatchObject({
      selectedClip: { id: ids.streamClip },
      selectedTrack: { id: ids.captionTrack },
    });
    await controller.preview();
    expect(port.preview).toHaveBeenCalledWith({
      projectId: ids.project,
      sequenceId: ids.sequence,
      sourceExcerptId: ids.excerpt,
      sourceExcerptRevision: "3",
      clipId: ids.streamClip,
      clipRevision: "2",
      trackId: ids.captionTrack,
      trackRevision: "4",
    });
    expect(controller.getSnapshot()).toMatchObject({ phase: "review", review: { cues: [{ text: "Opening" }] } });
    expect(await controller.apply()).toBeUndefined();
    expect(controller.getSnapshot()).toMatchObject({ phase: "error", canRetryIdenticalApply: true });
    const firstReview = vi.mocked(port.apply).mock.calls[0]?.[0];
    const firstInput = vi.mocked(port.apply).mock.calls[0]?.[1];

    expect((await controller.retryIdenticalApply())?.transactionId).toBe(ids.transaction);
    expect(vi.mocked(port.apply).mock.calls[1]?.[0]).toBe(firstReview);
    expect(vi.mocked(port.apply).mock.calls[1]?.[1]).toBe(firstInput);
    expect(controller.getSnapshot().phase).toBe("success");
  });

  it("invalidates revision drift and never retries a conflict", async () => {
    const port = captionPort();
    vi.mocked(port.apply).mockRejectedValueOnce(new CreatorEditError("conflict", 409));
    const controller = new CreatorCaptionController(port);
    const projection = {
      projectId: ids.project,
      sequenceId: ids.sequence,
      source: selection(),
      clips: [clip(ids.streamClip, ids.stream, 2)],
      alignments: [],
      tracks: [track(ids.captionTrack, "Captions")],
    } as const;
    controller.setProjection(projection);
    await controller.preview();
    await controller.apply();

    expect(controller.getSnapshot()).toMatchObject({ phase: "conflict", canRetryIdenticalApply: false });
    expect(controller.getSnapshot().selectedClip).toBeUndefined();
    expect(await controller.retryIdenticalApply()).toBeUndefined();

    controller.setProjection({
      ...projection,
      clips: [{ ...projection.clips[0], revision: revisionString("3") }],
    });
    expect(controller.getSnapshot()).toMatchObject({ phase: "ready", selectedClip: { revision: "3" } });
    expect(controller.getSnapshot().review).toBeUndefined();
  });
});

function captionPort(): CreatorCaptionPort {
  return {
    preview: vi.fn(async (input) => review(input.clipId)),
    apply: vi.fn(async () => receipt()),
  };
}

function selection() {
  return { sourceExcerpt: sourceExcerpt(), evidenceStatus: "exact" as const };
}

function sourceExcerpt(): SourceExcerpt {
  return {
    id: ids.excerpt,
    revision: revisionString("3"),
    documentId: ids.document,
    parentId: ids.root,
    assetId: ids.asset,
    acceptedFingerprint: digestString(`sha256:${"a".repeat(64)}`),
    sourceRange: range(1, 2),
    language: "en",
    effectiveText: "Opening",
    evidence: {
      artifactId: ids.transcript,
      sourceStreamId: ids.stream,
      segmentIds: [ids.segment],
      correctionRevisions: [],
    },
    tombstoned: false,
  };
}

function clip(id: Clip["id"], sourceStreamId: Clip["sourceStreamId"], timelineStart: number): Clip {
  return {
    id,
    revision: revisionString("2"),
    sequenceId: ids.sequence,
    trackId: ids.mediaTrack,
    assetId: ids.asset,
    sourceStreamId,
    sourceRange: range(0, 5),
    timelineRange: range(timelineStart, 5),
    enabled: true,
    tombstoned: false,
  };
}

function track(id: Track["id"], label: string): Track {
  return { id, revision: revisionString("4"), type: "caption", label };
}

function alignment(): Alignment {
  return {
    id: ids.alignment,
    revision: revisionString("2"),
    narrativeNodeId: ids.excerpt,
    narrativeNodeRevision: revisionString("3"),
    sequenceId: ids.sequence,
    status: "exact",
    targets: [
      {
        type: "clip",
        clip: { clipId: ids.alignedClip, clipRevision: revisionString("2"), localRange: range(0, 2) },
      },
    ],
  };
}

function review(clipId: Clip["id"]): CreatorCaptionReview {
  return {
    projectId: ids.project,
    sequenceId: ids.sequence,
    baseProjectRevision: revisionString("8"),
    activityCursor: cursorString("12"),
    language: "en",
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
    sourceExcerptId: ids.excerpt,
    clipId,
    trackId: ids.captionTrack,
    preconditionCount: 4,
    cues: [{ ordinal: 1, text: "Opening", sourceRange: range(1, 2), timelineRange: range(3, 2) }],
  };
}

function receipt(): CreatorEditCommit {
  return {
    proposalId: ids.proposal,
    transactionId: ids.transaction,
    committedProjectRevision: revisionString("9"),
    activityCursor: cursorString("13"),
    changes: [{ kind: "caption", id: ids.excerpt, revision: revisionString("1"), tombstoned: false }],
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
