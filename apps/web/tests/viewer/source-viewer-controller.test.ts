import type { MediaPlayerActuator } from "@open-cut/components";
import {
  digestString,
  durableID,
  int64String,
  revisionString,
  type SourcePositionResult,
  type SourcePreviewPreparation,
  uint64String,
  type ViewerMediaPort,
} from "@open-cut/contracts";
import { describe, expect, it, vi } from "vitest";

import {
  SourceViewerController,
  type SourceViewerRuntime,
  type SourceViewerSelection,
} from "../../src/lib/source-viewer-controller.js";

const projectId = durableID("018f0a60-7b80-7a01-8000-000000000001");
const assetId = durableID("018f0a60-7b80-7a01-8000-000000000002");
const videoStreamId = durableID("018f0a60-7b80-7a01-8000-000000000003");
const audioStreamId = durableID("018f0a60-7b80-7a01-8000-000000000004");
const jobId = durableID("018f0a60-7b80-7a01-8000-000000000005");
const artifactId = durableID("018f0a60-7b80-7a01-8000-000000000006");
const resourceId = durableID("018f0a60-7b80-7a01-8000-000000000007");
const fingerprint = digestString(`sha256:${"a".repeat(64)}`);

describe("SourceViewerController", () => {
  it("owns exact marks, bounded VFR settlement and finite selected-lane intersection", async () => {
    const port = viewerPort();
    vi.mocked(port.prepareSourcePreview).mockResolvedValue(readyPreparation());
    vi.mocked(port.resolveSourcePosition)
      .mockResolvedValueOnce(position("settle", "1", 2, "0", 1, "2", 1, false))
      .mockResolvedValueOnce(position("settle", "1", 2, "0", 1, "2", 1, false))
      .mockResolvedValueOnce(position("next", "0", 1, "3", 1, "5", 1, false));
    const controller = new SourceViewerController(port, new FakeRuntime());
    const actuator = mediaActuator(2.5);

    controller.open(selection());
    await settle();
    controller.attachActuator(actuator);
    expect(controller.getSnapshot()).toMatchObject({
      status: "ready",
      playhead: { value: "-2", scale: 1 },
      proxyPlayhead: { value: "0", scale: 1 },
    });

    await controller.captureIn();
    await controller.captureOut();
    expect(controller.selectedRange()).toEqual({
      start: { value: "0", scale: 1 },
      duration: { value: "3", scale: 1 },
    });
    expect(port.resolveSourcePosition).toHaveBeenNthCalledWith(1, projectId, assetId, selection(), {
      resourceId,
      operation: "settle",
      target: { value: "1", scale: 2 },
    });

    expect(controller.useFullSelectedSource()).toEqual({
      start: { value: "-1", scale: 1 },
      duration: { value: "4", scale: 1 },
    });
    expect(controller.getSnapshot().marks).toEqual({
      in: { value: "-1", scale: 1 },
      out: { value: "3", scale: 1 },
    });
  });

  it("clears marks when any pinned source identity changes", async () => {
    const port = viewerPort();
    vi.mocked(port.prepareSourcePreview).mockResolvedValue(readyPreparation());
    const controller = new SourceViewerController(port, new FakeRuntime());
    controller.open(selection());
    await settle();
    controller.useFullSelectedSource();

    controller.open({ ...selection(), audioStreamId: undefined });
    expect(controller.getSnapshot()).toMatchObject({ status: "preparing", marks: {} });
  });

  it("blocks full-source inference when a selected lane has no finite duration", async () => {
    const port = viewerPort();
    const preparation = readyPreparation();
    const audio = preparation.lease?.audio;
    if (!preparation.lease || !audio) throw new Error("fixture source lease has no audio timing");
    vi.mocked(port.prepareSourcePreview).mockResolvedValue({
      ...preparation,
      lease: { ...preparation.lease, audio: { ...audio, coverageDuration: undefined } },
    });
    const controller = new SourceViewerController(port, new FakeRuntime());
    controller.open(selection());
    await settle();
    expect(() => controller.useFullSelectedSource()).toThrow(/finite coverage/);
  });
});

function viewerPort(): ViewerMediaPort {
  return {
    prepareSourcePreview: vi.fn(),
    resolveSourcePosition: vi.fn(),
    prepareSequencePreview: vi.fn(),
    continueSequencePreview: vi.fn(),
    retrySequencePreview: vi.fn(),
  };
}

function selection(): SourceViewerSelection {
  return {
    projectId,
    assetId,
    assetRevision: revisionString("4"),
    fingerprint,
    videoStreamId,
    audioStreamId,
  };
}

function readyPreparation(): SourcePreviewPreparation {
  const videoStart = rational("-2", 1);
  const videoDuration = rational("7", 1);
  const audioStart = rational("-1", 1);
  const audioDuration = rational("4", 1);
  return {
    status: "ready",
    purpose: "source-preview",
    projectId,
    assetId,
    assetRevision: revisionString("4"),
    fingerprint,
    videoStreamId,
    audioStreamId,
    diagnostics: [],
    job: {
      id: jobId,
      kind: "proxy",
      state: "succeeded",
      progressBasisPoints: 10_000,
      prerequisites: [],
      resultArtifactId: artifactId,
      createdAt: "2026-07-16T00:00:00Z",
      updatedAt: "2026-07-16T00:00:01Z",
    },
    lease: {
      schema: "open-cut/media-lease/v1",
      resourceId,
      purpose: "source-preview",
      projectId,
      assetId,
      assetRevision: revisionString("4"),
      fingerprint,
      artifactId,
      artifactDigest: digestString(`sha256:${"b".repeat(64)}`),
      mimeType: "video/webm",
      byteLength: uint64String("4096"),
      etag: `"sha256-${"c".repeat(64)}"`,
      sameOriginUrl: `/api/v1/media/content/oc_media_${"A".repeat(43)}`,
      expiresAt: "1970-01-01T00:05:00Z",
      sourceEpoch: videoStart,
      video: {
        sourceStreamId: videoStreamId,
        coverageStart: videoStart,
        coverageDuration: videoDuration,
        sourceStartTime: videoStart,
        proxyStartTime: rational("0", 1),
        sourceTimeBase: rational("1", 1000),
        proxyTimeBase: rational("1", 1000),
      },
      audio: {
        sourceStreamId: audioStreamId,
        coverageStart: audioStart,
        coverageDuration: audioDuration,
        sourceStartTime: audioStart,
        proxyStartTime: rational("1", 1),
        sourceTimeBase: rational("1", 48000),
        proxyTimeBase: rational("1", 48000),
      },
    },
  };
}

function position(
  operation: "settle" | "previous" | "next",
  requestedValue: string,
  requestedScale: number,
  sourceValue: string,
  sourceScale: number,
  proxyValue: string,
  proxyScale: number,
  atEnd: boolean,
): SourcePositionResult {
  return {
    resourceId,
    projectId,
    assetId,
    assetRevision: revisionString("4"),
    fingerprint,
    videoStreamId,
    audioStreamId,
    operation,
    requestedTime: rational(requestedValue, requestedScale),
    sourceTime: rational(sourceValue, sourceScale),
    proxyTime: rational(proxyValue, proxyScale),
    boundary: "video-presentation",
    atStart: false,
    atEnd,
  };
}

function mediaActuator(currentTime: number): MediaPlayerActuator {
  return {
    readCurrentTimeSeconds: () => currentTime,
    seekToSeconds: vi.fn(),
    play: vi.fn(async () => undefined),
    pause: vi.fn(),
  };
}

function rational(value: string, scale: number) {
  return { value: int64String(value), scale };
}

class FakeRuntime implements SourceViewerRuntime {
  now = () => 0;
  schedule = vi.fn(() => () => undefined);
}

async function settle(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}
