// @vitest-environment jsdom

import { type Asset, digestString, durableID, int64String, revisionString, uint64String } from "@open-cut/contracts";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AssetSummary, TranscriptSurface } from "../../src/components/creator-workspace-media.js";

afterEach(cleanup);

describe("AssetSummary", () => {
  it("presents product readiness instead of internal blocked-job counts", () => {
    const pending = asset(false);
    const view = render(
      <AssetSummary
        asset={pending}
        onContext={vi.fn()}
        onPreview={vi.fn()}
        onTranscript={vi.fn()}
        previewAvailable
        selected={false}
      />,
    );

    expect(screen.getByText("Checking media")).toBeTruthy();
    expect(screen.queryByText(/blocked/)).toBeNull();
    expect((screen.getByRole("button", { name: "Open source" }) as HTMLButtonElement).disabled).toBe(true);

    view.unmount();
    render(
      <AssetSummary
        asset={asset(true)}
        onContext={vi.fn()}
        onPreview={vi.fn()}
        onTranscript={vi.fn()}
        previewAvailable
        selected={false}
      />,
    );
    expect(screen.getByText("Ready")).toBeTruthy();
    expect(screen.getByText("Transcript is waiting for local transcription support. Check System.")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Open source" }) as HTMLButtonElement).disabled).toBe(false);
  });
});

describe("TranscriptSurface", () => {
  it("orients the ready transcript before loading its bounded content", () => {
    const ready = asset(true);
    const onLoad = vi.fn();
    render(
      <TranscriptSurface
        asset={{
          ...ready,
          artifacts: [
            {
              id: durableID("018f0a60-7b80-7a01-8000-000000000305"),
              kind: "transcript",
              producerVersion: `transcript@sha256:${"b".repeat(64)}`,
              inputFingerprint: digestString(`sha256:${"a".repeat(64)}`),
              state: "ready",
              byteSize: uint64String("1024"),
              contentDigest: digestString(`sha256:${"c".repeat(64)}`),
              createdAt: "2026-07-22T00:00:00Z",
            },
          ],
        }}
        onContext={vi.fn()}
        onInspect={vi.fn()}
        onLoad={onLoad}
        onLoadMore={vi.fn()}
        onSelectDefault={vi.fn()}
        state={{ status: "idle" }}
      />,
    );

    expect(screen.getByRole("note").textContent).toContain("Transcript ready");
    expect(screen.getByRole("note").textContent).toContain("story.webm");
    fireEvent.click(screen.getByRole("button", { name: "Open transcript" }));
    expect(onLoad).toHaveBeenCalledOnce();
  });

  it("keeps transcript storage failures private and retains read recovery", () => {
    const ready = asset(true);
    const onLoad = vi.fn();
    render(
      <TranscriptSurface
        asset={ready}
        onContext={vi.fn()}
        onInspect={vi.fn()}
        onLoad={onLoad}
        onLoadMore={vi.fn()}
        onSelectDefault={vi.fn()}
        state={{ status: "unavailable", assetId: ready.id }}
      />,
    );

    expect(screen.getByText("Transcript data could not be loaded.")).toBeTruthy();
    expect(screen.queryByText(/sqlite|Application Support|\/Users\//i)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry transcript read" }));
    expect(onLoad).toHaveBeenCalledOnce();
  });

  it("leaves the no-source presentation to the owning panel", () => {
    const view = render(
      <TranscriptSurface
        asset={undefined}
        onContext={vi.fn()}
        onInspect={vi.fn()}
        onLoad={undefined}
        onLoadMore={vi.fn()}
        onSelectDefault={vi.fn()}
        state={{ status: "idle" }}
      />,
    );
    expect(view.container.childElementCount).toBe(0);
  });
});

function asset(ready: boolean): Asset {
  const projectId = durableID("018f0a60-7b80-7a01-8000-000000000301");
  const assetId = durableID("018f0a60-7b80-7a01-8000-000000000302");
  const streamId = durableID("018f0a60-7b80-7a01-8000-000000000303");
  const transcriptJobId = durableID("018f0a60-7b80-7a01-8000-000000000304");
  return {
    id: assetId,
    revision: revisionString("1"),
    projectId,
    displayName: "story.webm",
    importMode: "referenced",
    ...(ready
      ? {
          acceptedFingerprint: digestString(`sha256:${"a".repeat(64)}`),
          fingerprint: digestString(`sha256:${"a".repeat(64)}`),
          facts: {
            container: "matroska",
            containerAliases: ["webm"],
            duration: { value: int64String("4"), scale: 1 },
            streams: [
              {
                id: streamId,
                descriptor: {
                  index: 0,
                  mediaType: "video" as const,
                  codec: "vp9",
                  timeBase: { value: int64String("1"), scale: 1000 },
                  duration: { value: int64String("4"), scale: 1 },
                  dispositions: ["default"],
                  video: { width: 1920, height: 1080, rotation: 0 as const },
                },
              },
            ],
          },
        }
      : {}),
    tombstoned: false,
    availability: "online",
    artifacts: [],
    jobs: [
      {
        id: transcriptJobId,
        kind: "transcript",
        state: "blocked",
        progressBasisPoints: 0,
        prerequisites: [{ kind: "model-required", resourceId: "whisper-small" }],
        createdAt: "2026-07-22T00:00:00Z",
        updatedAt: "2026-07-22T00:00:00Z",
      },
    ],
  };
}
