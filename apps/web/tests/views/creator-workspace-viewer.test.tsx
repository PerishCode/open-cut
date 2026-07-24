// @vitest-environment jsdom

import {
  type Asset,
  digestString,
  durableID,
  int64String,
  type MediaRecoveryAction,
  revisionString,
  type SequencePreviewPreparation,
  uint64String,
} from "@open-cut/contracts";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { SequencePreviewSurface, SourcePreviewSurface } from "../../src/components/creator-workspace-viewer.js";
import type { SequenceViewerController, SequenceViewerSnapshot } from "../../src/lib/sequence-viewer-controller.js";
import type { SourceViewerController, SourceViewerSnapshot } from "../../src/lib/source-viewer-controller.js";

const projectId = durableID("018f0a60-7b80-7a01-8000-000000000001");
const sequenceId = durableID("018f0a60-7b80-7a01-8000-000000000002");
const jobId = durableID("018f0a60-7b80-7a01-8000-000000000003");
const artifactId = durableID("018f0a60-7b80-7a01-8000-000000000004");
const resourceId = durableID("018f0a60-7b80-7a01-8000-000000000005");

describe("SequencePreviewSurface", () => {
  afterEach(() => cleanup());

  it("keeps exact sequence transport persistently visible outside the media content", async () => {
    const controller = sequenceController();
    const view = render(
      <SequencePreviewSurface canvasLabel="1920 × 1080" controller={controller} snapshot={readySnapshot()} />,
    );

    const transport = screen.getByRole("region", { name: "Sequence transport" });
    const player = screen.getByLabelText("Main Sequence revision 14");
    expect(player.nextElementSibling?.contains(transport)).toBe(true);
    expect(within(transport).getByText("SEQ r14 · 00:08.00 / 00:09.00")).toBeTruthy();
    expect(within(transport).getByText(/1920 × 1080 · 30 FPS/)).toBeTruthy();
    expect(within(transport).queryByText(/PLAN/)).toBeNull();
    expect(player.getAttribute("controls")).toBeNull();
    expect(transport.getAttribute("aria-keyshortcuts")).toBe("Home ArrowLeft Space ArrowRight");
    expect(transport.tabIndex).toBe(0);

    fireEvent.click(within(transport).getByRole("button", { name: "Start" }));
    fireEvent.click(within(transport).getByRole("button", { name: "−1 frame" }));
    fireEvent.click(within(transport).getByRole("button", { name: "Play" }));
    fireEvent.click(within(transport).getByRole("button", { name: "+1 frame" }));

    await waitFor(() => expect(controller.seekToStart).toHaveBeenCalledTimes(1));
    expect(controller.stepFrame).toHaveBeenNthCalledWith(1, -1);
    expect(controller.stepFrame).toHaveBeenNthCalledWith(2, 1);
    expect(controller.togglePlayback).toHaveBeenCalledTimes(1);
    controller.seekToStart.mockClear();
    controller.stepFrame.mockClear();
    controller.togglePlayback.mockClear();

    fireEvent.keyDown(transport, { key: "Home" });
    fireEvent.keyDown(transport, { key: "ArrowLeft" });
    fireEvent.keyDown(transport, { key: " " });
    fireEvent.keyDown(transport, { key: "ArrowRight" });
    await waitFor(() => expect(controller.seekToStart).toHaveBeenCalledTimes(1));
    expect(controller.stepFrame).toHaveBeenNthCalledWith(1, -1);
    expect(controller.stepFrame).toHaveBeenNthCalledWith(2, 1);
    expect(controller.togglePlayback).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(within(transport).getByRole("button", { name: "Play" }), { key: " " });
    expect(controller.togglePlayback).toHaveBeenCalledTimes(1);

    view.rerender(
      <SequencePreviewSurface
        canvasLabel="1920 × 1080"
        controller={controller}
        snapshot={readySnapshot("9007199254740993", "9007199254740993")}
      />,
    );
    expect(
      within(screen.getByRole("region", { name: "Sequence transport" })).getByText(
        "SEQ r14 · 2501999792983:36:33.00 / 2501999792983:36:33.00",
      ),
    ).toBeTruthy();
  });

  it("translates preview recovery codes into Creator actions", async () => {
    const controller = sequenceController();
    const view = render(<SequencePreviewSurface controller={controller} snapshot={failedSnapshot("retry-job")} />);

    expect(screen.getByText("Sequence preview could not be prepared.")).toBeTruthy();
    expect(screen.queryByText(/sequence-preview-job-failed|retry-job/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry preview" }));
    expect(controller.retry).toHaveBeenCalledOnce();

    view.rerender(<SequencePreviewSurface controller={controller} snapshot={failedSnapshot("update-runtime")} />);
    expect(screen.getByText("Update Open Cut to preview this Sequence.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Retry preview" })).toBeNull();
  });

  it("keeps transport exceptions out of unavailable preview UI", () => {
    const controller = sequenceController();
    render(
      <SequencePreviewSurface
        controller={controller}
        snapshot={{
          ...readySnapshot(),
          status: "unavailable",
          preparation: undefined,
          error: new Error("prepare sequence preview failed with status 500"),
        }}
      />,
    );

    expect(screen.getByText("Sequence preview is unavailable.")).toBeTruthy();
    expect(screen.queryByText(/status 500/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Try preview again" }));
    expect(controller.restart).toHaveBeenCalledOnce();
  });
});

describe("SourcePreviewSurface", () => {
  afterEach(() => cleanup());

  it("keeps native scrubbing while adding focus-owned boundary and mark keys", async () => {
    const controller = sourceController();
    render(
      <SourcePreviewSurface
        asset={sourceAsset()}
        audioStreamId={undefined}
        controller={controller}
        onAudioStreamChange={() => undefined}
        onVideoStreamChange={() => undefined}
        snapshot={sourceReadySnapshot()}
        videoStreamId={resourceId}
      />,
    );

    expect(screen.getByRole("tab", { name: "Range" }).getAttribute("aria-selected")).toBe("true");
    const player = screen.getByLabelText("interview.webm source preview");
    const controls = screen.getByRole("region", { name: "Source range controls" });
    expect(player.hasAttribute("controls")).toBe(true);
    expect(controls.getAttribute("aria-keyshortcuts")).toBe("ArrowLeft ArrowRight I O");
    expect(controls.tabIndex).toBe(0);
    expect(within(controls).getByText("SOURCE 00:00.00 · PROXY 00:00.00")).toBeTruthy();
    expect(within(controls).getByText("IN — · OUT —")).toBeTruthy();

    fireEvent.keyDown(controls, { key: "ArrowLeft" });
    fireEvent.keyDown(controls, { key: "ArrowRight" });
    fireEvent.keyDown(controls, { key: "i" });
    fireEvent.keyDown(controls, { key: "O", shiftKey: true });
    await waitFor(() => expect(controller.step).toHaveBeenCalledTimes(2));
    expect(controller.step).toHaveBeenNthCalledWith(1, "previous");
    expect(controller.step).toHaveBeenNthCalledWith(2, "next");
    expect(controller.captureIn).toHaveBeenCalledTimes(1);
    expect(controller.captureOut).toHaveBeenCalledTimes(1);

    fireEvent.keyDown(within(controls).getByRole("button", { name: "Mark In" }), { key: "i" });
    expect(controller.captureIn).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole("tab", { name: "Streams" }));
    expect(screen.getByRole("tab", { name: "Streams" }).getAttribute("aria-selected")).toBe("true");
    expect(screen.getByRole("region", { name: "VIDEO source stream" })).toBeTruthy();
    expect(screen.getByRole("region", { name: "AUDIO source stream" })).toBeTruthy();
    expect(screen.queryByRole("region", { name: "Source range controls" })).toBeNull();
  });

  it("opens stream selection when the source has no explicit streams selected", () => {
    render(
      <SourcePreviewSurface
        asset={sourceAsset()}
        audioStreamId={undefined}
        controller={sourceController()}
        onAudioStreamChange={() => undefined}
        onVideoStreamChange={() => undefined}
        snapshot={sourceReadySnapshot()}
        videoStreamId={undefined}
      />,
    );

    expect(screen.getByRole("tab", { name: "Streams" }).getAttribute("aria-selected")).toBe("true");
    expect(screen.getByRole("region", { name: "VIDEO source stream" })).toBeTruthy();
    expect(screen.getByRole("region", { name: "AUDIO source stream" })).toBeTruthy();
    expect(screen.queryByLabelText("interview.webm source preview")).toBeNull();
  });

  it("separates retryable transport loss from a source that needs relinking", () => {
    const controller = sourceController();
    const view = render(
      <SourcePreviewSurface
        asset={sourceAsset()}
        audioStreamId={undefined}
        controller={controller}
        onAudioStreamChange={() => undefined}
        onVideoStreamChange={() => undefined}
        snapshot={{
          ...sourceReadySnapshot(),
          status: "unavailable",
          preparation: undefined,
          error: new Error("prepare source preview failed with status 502"),
        }}
        videoStreamId={resourceId}
      />,
    );

    expect(screen.getByText("Source preview is unavailable.")).toBeTruthy();
    expect(screen.queryByText(/status 502/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Try preview again" }));
    expect(controller.wake).toHaveBeenCalledOnce();

    view.rerender(
      <SourcePreviewSurface
        asset={sourceAsset()}
        audioStreamId={undefined}
        controller={controller}
        onAudioStreamChange={() => undefined}
        onVideoStreamChange={() => undefined}
        snapshot={sourceFailedSnapshot()}
        videoStreamId={resourceId}
      />,
    );
    expect(screen.getByText("Relink this source before previewing it again.")).toBeTruthy();
    expect(screen.queryByText(/proxy failed/i)).toBeNull();
    expect(screen.queryByRole("button", { name: "Try preview again" })).toBeNull();
  });
});

function sequenceController() {
  return {
    attachActuator: vi.fn(),
    observePlaybackPosition: vi.fn(),
    restart: vi.fn(),
    retry: vi.fn(),
    seekToStart: vi.fn(),
    setPlaying: vi.fn(),
    stepFrame: vi.fn(),
    syncActuator: vi.fn(),
    togglePlayback: vi.fn(async () => undefined),
    wake: vi.fn(),
  } as unknown as SequenceViewerController & {
    seekToStart: ReturnType<typeof vi.fn>;
    retry: ReturnType<typeof vi.fn>;
    stepFrame: ReturnType<typeof vi.fn>;
    togglePlayback: ReturnType<typeof vi.fn>;
  };
}

function sourceController() {
  return {
    attachActuator: vi.fn(),
    captureIn: vi.fn(async () => undefined),
    captureOut: vi.fn(async () => undefined),
    clearMarks: vi.fn(),
    hasFiniteSelectedCoverage: vi.fn(() => true),
    selectedRange: vi.fn(() => undefined),
    setPlaying: vi.fn(),
    settleActuator: vi.fn(async () => undefined),
    step: vi.fn(async () => undefined),
    useFullSelectedSource: vi.fn(),
    wake: vi.fn(),
  } as unknown as SourceViewerController & {
    captureIn: ReturnType<typeof vi.fn>;
    captureOut: ReturnType<typeof vi.fn>;
    step: ReturnType<typeof vi.fn>;
    wake: ReturnType<typeof vi.fn>;
  };
}

function sourceAsset(): Asset {
  return {
    id: resourceId,
    revision: revisionString("1"),
    projectId,
    displayName: "interview.webm",
    importMode: "referenced",
    acceptedFingerprint: digestString(`sha256:${"d".repeat(64)}`),
    tombstoned: false,
    availability: "online",
    artifacts: [],
    jobs: [],
    facts: { container: "matroska", containerAliases: ["webm"], streams: [] },
  };
}

function sourceReadySnapshot(): SourceViewerSnapshot {
  return {
    status: "ready",
    marks: {},
    playback: "paused",
    playhead: { value: int64String("0"), scale: 1 },
    proxyPlayhead: { value: int64String("0"), scale: 1 },
    preparation: {
      status: "ready",
      lease: {
        mimeType: "video/webm",
        byteLength: uint64String("4096"),
        sameOriginUrl: `/api/v1/media/content/oc_source_${"A".repeat(43)}`,
      },
    },
  } as SourceViewerSnapshot;
}

function sourceFailedSnapshot(): SourceViewerSnapshot {
  return {
    status: "failed",
    marks: {},
    playback: "paused",
    preparation: {
      status: "failed",
      purpose: "source-preview",
      projectId,
      assetId: resourceId,
      assetRevision: revisionString("1"),
      fingerprint: digestString(`sha256:${"d".repeat(64)}`),
      videoStreamId: resourceId,
      diagnostics: [
        {
          code: "source-proxy-job-failed",
          severity: "blocking",
          subjectKind: "media-job",
          subjectId: jobId,
          recovery: "relink-source",
        },
      ],
      stage: "proxy",
      job: {
        id: jobId,
        kind: "proxy",
        state: "failed",
        progressBasisPoints: 10_000,
        prerequisites: [],
        terminalErrorCode: "proxy-failed",
        createdAt: "2026-07-16T00:00:00Z",
        updatedAt: "2026-07-16T00:00:01Z",
      },
    },
  };
}

function readySnapshot(playhead = "8", duration = "9"): SequenceViewerSnapshot {
  const preparation = readyPreparation(duration);
  return {
    status: "ready",
    projectId,
    sequenceId,
    pinnedRevision: revisionString("14"),
    availableRevision: revisionString("14"),
    playhead: { value: int64String(playhead), scale: 1 },
    playback: "paused",
    preparation,
  };
}

function failedSnapshot(recovery: MediaRecoveryAction): SequenceViewerSnapshot {
  const planDigest = digestString(`sha256:${"a".repeat(64)}`);
  return {
    status: "failed",
    projectId,
    sequenceId,
    pinnedRevision: revisionString("14"),
    availableRevision: revisionString("14"),
    playhead: { value: int64String("0"), scale: 1 },
    playback: "paused",
    preparation: {
      status: "failed",
      purpose: "sequence-preview",
      projectId,
      sequenceId,
      sequenceRevision: revisionString("14"),
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
    },
  };
}

function readyPreparation(duration: string): SequencePreviewPreparation {
  const planDigest = digestString(`sha256:${"a".repeat(64)}`);
  return {
    status: "ready",
    purpose: "sequence-preview",
    projectId,
    sequenceId,
    sequenceRevision: revisionString("14"),
    continuation: { jobId, renderPlanDigest: planDigest },
    diagnostics: [],
    lease: {
      schema: "open-cut/media-lease/v1",
      resourceId,
      purpose: "sequence-preview",
      projectId,
      sequenceId,
      sequenceRevision: revisionString("14"),
      renderPlanDigest: planDigest,
      artifactId,
      artifactDigest: digestString(`sha256:${"b".repeat(64)}`),
      facts: {
        semanticDuration: { value: int64String(duration), scale: 1 },
        presentationDuration: { value: int64String("271"), scale: 30 },
        canvasWidth: 1280,
        canvasHeight: 720,
        frameRate: { value: int64String("30"), scale: 1 },
        videoFrameCount: uint64String("271"),
        audioSampleRate: 48_000,
        audioSampleCount: uint64String("433600"),
        videoCodec: "vp9",
        audioCodec: "opus",
        pixelFormat: "yuv420p",
        channelLayout: "stereo",
      },
      mimeType: "video/webm",
      byteLength: uint64String("4096"),
      etag: `"sha256-${"c".repeat(64)}"`,
      sameOriginUrl: `/api/v1/media/content/oc_sequence_${"A".repeat(43)}`,
      expiresAt: "2026-07-23T06:00:00Z",
    },
  };
}
