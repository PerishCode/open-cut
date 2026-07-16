// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";

import { createContracts, durableID } from "../src/index.js";

const ids = {
  project: "018f0a60-7b80-7a01-8000-000000000001",
  asset: "018f0a60-7b80-7a01-8000-000000000002",
  stream: "018f0a60-7b80-7a01-8000-000000000003",
  artifact: "018f0a60-7b80-7a01-8000-000000000004",
  segment: "018f0a60-7b80-7a01-8000-000000000005",
  token: "018f0a60-7b80-7a01-8000-000000000006",
  correction: "018f0a60-7b80-7a01-8000-000000000007",
} as const;

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("transcript media contracts", () => {
  it("returns bounded semantic recognition without adapter or resource details", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => response(transcriptPage())),
    );
    const page = await createContracts().media.read.transcript(durableID(ids.project), durableID(ids.asset), {
      limit: 20,
    });
    expect(fetch).toHaveBeenCalledWith(
      `/api/v1/projects/${ids.project}/assets/${ids.asset}/transcript?limit=20`,
      expect.objectContaining({ signal: undefined }),
    );
    expect(page).toMatchObject({
      schema: "open-cut/transcript-read/v1",
      artifact: {
        id: ids.artifact,
        assetId: ids.asset,
        detectedLanguage: "en-US",
        normalizedSampleCount: "32000",
      },
      segments: [{ ordinal: 0, text: "Hello world." }],
      corrections: [{ id: ids.correction, revision: "2", originalText: "Hello world.", effectiveText: "Hello world!" }],
      activityCursor: "7",
    });
    expect(page.artifact).not.toHaveProperty("resourceId");
    expect(page.artifact).not.toHaveProperty("contentDigest");
  });

  it("rejects discontinuous or fabricated lexical evidence", async () => {
    for (const payload of [
      transcriptPage({ segmentOrdinal: 1 }),
      transcriptPage({ segmentText: "Different" }),
      transcriptPage({ language: "en-us" }),
    ]) {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => response(payload)),
      );
      await expect(
        createContracts().media.read.transcript(durableID(ids.project), durableID(ids.asset)),
      ).rejects.toThrow(/transcript/);
    }
  });

  it("keeps default selection Creator-only and compare-and-swap exact", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        expect(init?.method).toBe("PUT");
        expect(JSON.parse(String(init?.body))).toEqual({
          artifactId: ids.artifact,
          expectedDefaultArtifactId: ids.token,
        });
        return response({
          assetId: ids.asset,
          artifactId: ids.artifact,
          previousArtifactId: ids.token,
          selectedAt: "2026-07-15T10:00:00Z",
          activityCursor: "8",
          replayed: false,
        });
      }),
    );
    await expect(
      createContracts().media.write.selectTranscriptDefault(durableID(ids.project), durableID(ids.asset), {
        artifactId: durableID(ids.artifact),
        expectedDefaultArtifactId: durableID(ids.token),
      }),
    ).resolves.toMatchObject({
      assetId: ids.asset,
      artifactId: ids.artifact,
      previousArtifactId: ids.token,
      activityCursor: "8",
      replayed: false,
    });
  });
});

function transcriptPage(
  overrides: Readonly<{ segmentOrdinal?: number; segmentText?: string; language?: string }> = {},
) {
  return {
    schema: "open-cut/transcript-read/v1",
    artifact: {
      id: ids.artifact,
      assetId: ids.asset,
      sourceStreamId: ids.stream,
      recognitionProfile: "whisper-small-multilingual-v1",
      engineVersion: `transcript@sha256:${"a".repeat(64)}`,
      engineTarget: "mac-arm64",
      modelName: "whisper-small-multilingual-v1",
      modelVersion: "whisper-small@c521a4b",
      detectedLanguage: overrides.language ?? "en-US",
      languageConfidenceBasisPoints: 9_500,
      sourceStartTime: { value: "0", scale: 1 },
      normalizedSampleCount: "32000",
      isDefault: true,
      createdAt: "2026-07-15T09:00:00Z",
    },
    segments: [
      {
        id: ids.segment,
        ordinal: overrides.segmentOrdinal ?? 0,
        sourceRange: { start: { value: "0", scale: 1 }, duration: { value: "2", scale: 1 } },
        text: overrides.segmentText ?? "Hello world.",
        tokens: [
          {
            id: ids.token,
            sourceRange: { start: { value: "0", scale: 1 }, duration: { value: "2", scale: 1 } },
            text: "Hello world.",
            confidenceBasisPoints: 9_800,
          },
        ],
      },
    ],
    corrections: [
      {
        id: ids.correction,
        revision: "2",
        segmentIds: [ids.segment],
        sourceRange: { start: { value: "0", scale: 1 }, duration: { value: "2", scale: 1 } },
        originalText: "Hello world.",
        effectiveText: "Hello world!",
        language: "en-US",
      },
    ],
    activityCursor: "7",
  };
}

function response(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}
