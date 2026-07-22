// @vitest-environment jsdom

import { type Asset, digestString, durableID, int64String, revisionString } from "@open-cut/contracts";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AssetSummary } from "../../src/components/creator-workspace-media.js";

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
