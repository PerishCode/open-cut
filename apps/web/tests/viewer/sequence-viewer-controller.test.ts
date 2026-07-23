import type { MediaPlayerActuator } from "@open-cut/components";
import {
  digestString,
  durableID,
  int64String,
  revisionString,
  type SequencePreviewPreparation,
  uint64String,
  type ViewerMediaPort,
} from "@open-cut/contracts";
import { describe, expect, it, vi } from "vitest";

import { SequenceViewerController, type SequenceViewerRuntime } from "../../src/lib/sequence-viewer-controller.js";

const projectId = durableID("018f0a60-7b80-7a01-8000-000000000001");
const sequenceId = durableID("018f0a60-7b80-7a01-8000-000000000002");
const jobId = durableID("018f0a60-7b80-7a01-8000-000000000003");
const artifactId = durableID("018f0a60-7b80-7a01-8000-000000000004");
const resourceId = durableID("018f0a60-7b80-7a01-8000-000000000005");
const planDigest = digestString(`sha256:${"a".repeat(64)}`);

describe("SequenceViewerController", () => {
  it("keeps a moving head separate and adopts it through a new generation", async () => {
    const first = deferred<SequencePreviewPreparation>();
    const second = deferred<SequencePreviewPreparation>();
    const port = viewerPort();
    vi.mocked(port.prepareSequencePreview)
      .mockImplementationOnce(() => first.promise)
      .mockImplementationOnce(() => second.promise);
    const runtime = new FakeRuntime();
    const controller = new SequenceViewerController(port, runtime);

    controller.open(projectId, sequenceId, revisionString("1"));
    controller.setAvailableRevision(revisionString("2"));
    expect(controller.getSnapshot()).toMatchObject({ pinnedRevision: "1", availableRevision: "2" });

    first.resolve(readyPreparation("1", "5"));
    await settle();
    controller.setPlayhead({ value: int64String("4"), scale: 1 });
    controller.setPlaying(true);
    controller.adoptRevision(revisionString("2"));
    expect(controller.getSnapshot()).toMatchObject({
      status: "preparing",
      pinnedRevision: "2",
      availableRevision: "2",
      playback: "paused",
      playhead: { value: "4", scale: 1 },
    });
    second.resolve(readyPreparation("2", "3"));
    await settle();
    expect(controller.getSnapshot()).toMatchObject({
      status: "ready",
      pinnedRevision: "2",
      playhead: { value: "3", scale: 1 },
    });
  });

  it("polls and renews only through the exact continuation", async () => {
    const port = viewerPort();
    vi.mocked(port.prepareSequencePreview).mockResolvedValue(preparingPreparation("1"));
    vi.mocked(port.continueSequencePreview).mockResolvedValue(readyPreparation("1", "5"));
    const runtime = new FakeRuntime();
    const controller = new SequenceViewerController(port, runtime);

    controller.open(projectId, sequenceId, revisionString("1"));
    await settle();
    expect(controller.getSnapshot().status).toBe("preparing");
    runtime.advance(1_000);
    await settle();
    expect(port.continueSequencePreview).toHaveBeenCalledWith(
      projectId,
      sequenceId,
      {
        expectedSequenceRevision: "1",
        continuation: { jobId, renderPlanDigest: planDigest },
      },
      expect.any(AbortSignal),
    );
    expect(controller.getSnapshot().status).toBe("ready");
    runtime.advance(270_000);
    await settle();
    expect(port.continueSequencePreview).toHaveBeenCalledTimes(2);
    expect(port.prepareSequencePreview).toHaveBeenCalledTimes(1);
  });

  it("exposes retry only for a closed retry-job recovery", async () => {
    const port = viewerPort();
    vi.mocked(port.prepareSequencePreview).mockResolvedValue(failedPreparation("1", "retry-job"));
    vi.mocked(port.retrySequencePreview).mockResolvedValue(preparingPreparation("1"));
    const controller = new SequenceViewerController(port, new FakeRuntime());

    controller.open(projectId, sequenceId, revisionString("1"));
    await settle();
    expect(controller.getSnapshot().status).toBe("failed");
    controller.retry();
    await settle();
    expect(port.retrySequencePreview).toHaveBeenCalledWith(
      projectId,
      sequenceId,
      {
        expectedSequenceRevision: "1",
        continuation: { jobId, renderPlanDigest: planDigest },
      },
      expect.any(AbortSignal),
    );
    expect(controller.getSnapshot().status).toBe("preparing");
  });

  it("owns exact seek, frame-step, and playback transport over one media actuator", async () => {
    const port = viewerPort();
    vi.mocked(port.prepareSequencePreview).mockResolvedValue(readyPreparation("1", "7200"));
    const controller = new SequenceViewerController(port, new FakeRuntime());
    const actuator = mediaActuator();

    controller.open(projectId, sequenceId, revisionString("1"));
    await settle();
    controller.attachActuator(actuator);
    controller.setPlayhead({ value: int64String("8"), scale: 1 });
    expect(actuator.seekToSeconds).toHaveBeenLastCalledWith(8);
    controller.observePlaybackPosition(8.033333);
    expect(controller.getSnapshot().playhead).toEqual({ value: "241", scale: 30 });

    controller.observePlaybackPosition(3_600.1);
    expect(controller.getSnapshot().playhead).toEqual({ value: "36001", scale: 10 });

    controller.stepFrame(-1);
    expect(actuator.pause).toHaveBeenCalledTimes(1);
    expect(controller.getSnapshot().playhead).toEqual({ value: "54001", scale: 15 });
    controller.stepFrame(1);
    expect(controller.getSnapshot().playhead).toEqual({ value: "36001", scale: 10 });

    controller.seekToStart();
    expect(controller.getSnapshot().playhead).toEqual({ value: "0", scale: 1 });
    controller.setPlayhead({ value: int64String("7200"), scale: 1 });
    await controller.play();
    expect(controller.getSnapshot().playhead).toEqual({ value: "0", scale: 1 });
    expect(actuator.play).toHaveBeenCalledTimes(1);
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

function preparingPreparation(revision: string): SequencePreviewPreparation {
  return {
    status: "preparing",
    purpose: "sequence-preview",
    projectId,
    sequenceId,
    sequenceRevision: revisionString(revision),
    continuation: { jobId, renderPlanDigest: planDigest },
    stage: "render",
    diagnostics: [],
    job: previewJob("running"),
  };
}

function failedPreparation(revision: string, recovery: "retry-job" | "update-runtime"): SequencePreviewPreparation {
  return {
    status: "failed",
    purpose: "sequence-preview",
    projectId,
    sequenceId,
    sequenceRevision: revisionString(revision),
    continuation: { jobId, renderPlanDigest: planDigest },
    stage: "render",
    diagnostics: [
      {
        code: "sequence-preview-job-failed",
        severity: "blocking",
        subjectKind: "work-job",
        subjectId: jobId,
        recovery,
      },
    ],
    job: { ...previewJob("failed"), terminalErrorCode: "renderer-failed" },
  };
}

function readyPreparation(revision: string, duration: string): SequencePreviewPreparation {
  return {
    status: "ready",
    purpose: "sequence-preview",
    projectId,
    sequenceId,
    sequenceRevision: revisionString(revision),
    continuation: { jobId, renderPlanDigest: planDigest },
    diagnostics: [],
    job: { ...previewJob("succeeded"), progressBasisPoints: 10_000, resultArtifactId: artifactId },
    lease: {
      schema: "open-cut/media-lease/v1",
      resourceId,
      purpose: "sequence-preview",
      projectId,
      sequenceId,
      sequenceRevision: revisionString(revision),
      renderPlanDigest: planDigest,
      artifactId,
      artifactDigest: digestString(`sha256:${"b".repeat(64)}`),
      facts: {
        semanticDuration: { value: int64String(duration), scale: 1 },
        presentationDuration: { value: int64String(duration), scale: 1 },
        canvasWidth: 1280,
        canvasHeight: 720,
        frameRate: { value: int64String("30"), scale: 1 },
        videoFrameCount: uint64String("150"),
        audioSampleRate: 48_000,
        audioSampleCount: uint64String("240000"),
        videoCodec: "vp9",
        audioCodec: "opus",
        pixelFormat: "yuv420p",
        channelLayout: "stereo",
      },
      mimeType: "video/webm",
      byteLength: uint64String("4096"),
      etag: `"sha256-${"c".repeat(64)}"`,
      sameOriginUrl: `/api/v1/media/content/oc_sequence_${"A".repeat(43)}`,
      expiresAt: "1970-01-01T00:05:00Z",
    },
  };
}

function previewJob(state: "running" | "failed" | "succeeded") {
  return {
    id: jobId,
    kind: "sequence-preview" as const,
    state,
    progressBasisPoints: state === "running" ? 2_500 : 0,
    renderPlanDigest: planDigest,
    createdAt: "2026-07-15T09:00:00Z",
    updatedAt: "2026-07-15T09:01:00Z",
  };
}

class FakeRuntime implements SequenceViewerRuntime {
  nowValue = 0;
  timers: Array<{ at: number; callback: () => void; cancelled: boolean }> = [];

  now = () => this.nowValue;

  schedule = (callback: () => void, delay: number): (() => void) => {
    const timer = { at: this.nowValue + delay, callback, cancelled: false };
    this.timers.push(timer);
    return () => {
      timer.cancelled = true;
    };
  };

  advance(milliseconds: number): void {
    this.nowValue += milliseconds;
    const ready = this.timers.filter((timer) => !timer.cancelled && timer.at <= this.nowValue);
    for (const timer of ready) {
      timer.cancelled = true;
      timer.callback();
    }
  }
}

function deferred<Value>() {
  let resolve!: (value: Value) => void;
  const promise = new Promise<Value>((accept) => {
    resolve = accept;
  });
  return { promise, resolve };
}

async function settle(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

function mediaActuator(): MediaPlayerActuator {
  return {
    readCurrentTimeSeconds: vi.fn(() => 0),
    seekToSeconds: vi.fn(),
    play: vi.fn(async () => undefined),
    pause: vi.fn(),
  };
}
