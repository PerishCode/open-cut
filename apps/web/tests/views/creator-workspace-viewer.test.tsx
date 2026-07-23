// @vitest-environment jsdom

import {
  digestString,
  durableID,
  int64String,
  revisionString,
  type SequencePreviewPreparation,
  uint64String,
} from "@open-cut/contracts";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { SequencePreviewSurface } from "../../src/components/creator-workspace-viewer.js";
import type { SequenceViewerController, SequenceViewerSnapshot } from "../../src/lib/sequence-viewer-controller.js";

const projectId = durableID("018f0a60-7b80-7a01-8000-000000000001");
const sequenceId = durableID("018f0a60-7b80-7a01-8000-000000000002");
const jobId = durableID("018f0a60-7b80-7a01-8000-000000000003");
const artifactId = durableID("018f0a60-7b80-7a01-8000-000000000004");
const resourceId = durableID("018f0a60-7b80-7a01-8000-000000000005");

describe("SequencePreviewSurface", () => {
  afterEach(() => cleanup());

  it("keeps exact sequence transport persistently visible outside the media content", async () => {
    const controller = sequenceController();
    const view = render(<SequencePreviewSurface controller={controller} snapshot={readySnapshot()} />);

    const transport = screen.getByRole("region", { name: "Sequence transport" });
    const player = screen.getByLabelText("Main Sequence revision 14");
    expect(player.nextElementSibling?.contains(transport)).toBe(true);
    expect(within(transport).getByText("SEQUENCE r14 · 00:08.00 / 00:09.00")).toBeTruthy();
    expect(player.getAttribute("controls")).toBeNull();

    fireEvent.click(within(transport).getByRole("button", { name: "Go to start" }));
    fireEvent.click(within(transport).getByRole("button", { name: "Previous frame" }));
    fireEvent.click(within(transport).getByRole("button", { name: "Play" }));
    fireEvent.click(within(transport).getByRole("button", { name: "Next frame" }));

    await waitFor(() => expect(controller.seekToStart).toHaveBeenCalledTimes(1));
    expect(controller.stepFrame).toHaveBeenNthCalledWith(1, -1);
    expect(controller.stepFrame).toHaveBeenNthCalledWith(2, 1);
    expect(controller.togglePlayback).toHaveBeenCalledTimes(1);

    view.rerender(
      <SequencePreviewSurface
        controller={controller}
        snapshot={readySnapshot("9007199254740993", "9007199254740993")}
      />,
    );
    expect(
      within(screen.getByRole("region", { name: "Sequence transport" })).getByText(
        "SEQUENCE r14 · 2501999792983:36:33.00 / 2501999792983:36:33.00",
      ),
    ).toBeTruthy();
  });
});

function sequenceController() {
  return {
    attachActuator: vi.fn(),
    observePlaybackPosition: vi.fn(),
    seekToStart: vi.fn(),
    setPlaying: vi.fn(),
    stepFrame: vi.fn(),
    syncActuator: vi.fn(),
    togglePlayback: vi.fn(async () => undefined),
    wake: vi.fn(),
  } as unknown as SequenceViewerController & {
    seekToStart: ReturnType<typeof vi.fn>;
    stepFrame: ReturnType<typeof vi.fn>;
    togglePlayback: ReturnType<typeof vi.fn>;
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
