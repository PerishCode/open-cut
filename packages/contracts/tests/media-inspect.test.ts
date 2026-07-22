// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";

import { createContracts, durableID } from "../src/index.js";

const ids = {
  project: "018f0a70-7b80-7a01-8000-000000000001",
  asset: "018f0a70-7b80-7a01-8000-000000000002",
  artifact: "018f0a70-7b80-7a01-8000-000000000003",
  job: "018f0a70-7b80-7a01-8000-000000000004",
} as const;

const digest = `sha256:${"a".repeat(64)}`;

function response(value: unknown): Response {
  return new Response(JSON.stringify(value), {
    status: 200,
    headers: { "content-type": "application/json" },
  });
}

function assetDetail(artifactKind: string, jobKind: string) {
  return {
    activityCursor: "9",
    asset: {
      id: ids.asset,
      revision: "3",
      projectId: ids.project,
      displayName: "footage.webm",
      importMode: "referenced",
      tombstoned: false,
      availability: "online",
      artifacts: [
        {
          id: ids.artifact,
          kind: artifactKind,
          producerVersion: "producer/v1",
          inputFingerprint: digest,
          state: "ready",
          byteSize: "1024",
          contentDigest: digest,
          createdAt: "2026-07-15T09:00:00Z",
        },
      ],
      jobs: [
        {
          id: ids.job,
          kind: jobKind,
          state: "succeeded",
          progressBasisPoints: 10000,
          prerequisites: [],
          createdAt: "2026-07-15T09:00:00Z",
          updatedAt: "2026-07-15T09:00:00Z",
        },
      ],
    },
  };
}

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("asset inspect artifact and job kinds", () => {
  // The export pipeline produces a render-input artifact and a render-input
  // job, both of which land in the asset's most-recent-32 window. domain's
  // ArtifactSummary and MediaJobSummary enums both include render-input, but
  // Contracts omitted it — so inspecting any asset that had been exported threw
  // "media artifact kind is invalid", the Sequence preview never left
  // Preparing, and the Save As action never appeared. The installed journey hit
  // this every run once it produced an export.
  it("accepts render-input artifacts and jobs the export pipeline produces", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => response(assetDetail("render-input", "render-input"))),
    );
    const asset = await createContracts().media.read.inspect(durableID(ids.project), durableID(ids.asset));
    expect(asset.artifacts[0]?.kind).toBe("render-input");
    expect(asset.jobs[0]?.kind).toBe("render-input");
  });

  it("still accepts every other artifact and job kind domain exposes", async () => {
    for (const kind of ["media-facts", "frame-sample-set", "proxy", "waveform", "transcript"]) {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => response(assetDetail(kind, kind === "media-facts" ? "probe" : kind))),
      );
      const asset = await createContracts().media.read.inspect(durableID(ids.project), durableID(ids.asset));
      expect(asset.artifacts[0]?.kind).toBe(kind);
    }
  });

  it("still rejects a kind domain does not define", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => response(assetDetail("waveform", "sharpen"))),
    );
    await expect(createContracts().media.read.inspect(durableID(ids.project), durableID(ids.asset))).rejects.toThrow(
      /media job kind is invalid/,
    );
  });
});
