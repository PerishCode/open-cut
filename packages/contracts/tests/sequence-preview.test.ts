// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";

import { createContracts, digestString, durableID, revisionString } from "../src/index.js";

const ids = {
  project: "018f0a60-7b80-7a01-8000-000000000001",
  sequence: "018f0a60-7b80-7a01-8000-000000000003",
  job: "018f0a60-7b80-7001-8000-00000000000d",
  artifact: "018f0a60-7b80-7001-8000-00000000000e",
  resource: "018f0a60-7b80-7001-8000-00000000000f",
  retryJob: "018f0a60-7b80-7001-8000-000000000010",
} as const;

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("sequence preview media contracts", () => {
  it("round-trips the exact sequence preview continuation instead of adopting newer work", async () => {
    const renderPlanDigest = `sha256:${"c".repeat(64)}`;
    let requests = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        requests += 1;
        expect(JSON.parse(String(init?.body))).toEqual({
          purpose: "sequence-preview",
          operation: requests === 1 ? "prepare" : "continue",
          expectedSequenceRevision: "2",
          ...(requests === 1
            ? {}
            : {
                continuation: {
                  jobId: ids.job,
                  renderPlanDigest,
                },
              }),
        });
        const succeeded = requests === 2;
        return jsonResponse({
          status: succeeded ? "ready" : "preparing",
          purpose: "sequence-preview",
          projectId: ids.project,
          sequenceId: ids.sequence,
          sequenceRevision: "2",
          diagnostics: [],
          continuation: { jobId: ids.job, renderPlanDigest },
          job: {
            id: ids.job,
            kind: "sequence-preview",
            state: succeeded ? "succeeded" : "running",
            progressBasisPoints: succeeded ? 10_000 : 2_500,
            renderPlanDigest,
            ...(succeeded ? { resultArtifactId: ids.artifact } : {}),
            createdAt: "2026-07-15T09:00:00Z",
            updatedAt: "2026-07-15T09:01:00Z",
          },
          ...(succeeded
            ? {
                lease: {
                  schema: "open-cut/media-lease/v1",
                  resourceId: ids.resource,
                  purpose: "sequence-preview",
                  projectId: ids.project,
                  sequenceId: ids.sequence,
                  sequenceRevision: "2",
                  renderPlanDigest,
                  artifactId: ids.artifact,
                  artifactDigest: `sha256:${"d".repeat(64)}`,
                  facts: {
                    semanticDuration: { value: "2", scale: 1 },
                    presentationDuration: { value: "2", scale: 1 },
                    canvasWidth: 1920,
                    canvasHeight: 1080,
                    frameRate: { value: "30000", scale: 1001 },
                    videoFrameCount: "60",
                    audioSampleRate: 48_000,
                    audioSampleCount: "96000",
                    videoCodec: "vp9",
                    audioCodec: "opus",
                    pixelFormat: "yuv420p",
                    channelLayout: "stereo",
                  },
                  mimeType: "video/webm",
                  byteLength: "4096",
                  etag: `"sha256-${"e".repeat(64)}"`,
                  sameOriginUrl: `/api/v1/media/content/oc_sequence_${"A".repeat(43)}`,
                  expiresAt: "2026-07-15T09:05:00Z",
                },
              }
            : { stage: "render" }),
        });
      }),
    );
    const viewer = createContracts().media.viewer;
    const preparing = await viewer.prepareSequencePreview(durableID(ids.project), durableID(ids.sequence), {
      expectedSequenceRevision: revisionString("2"),
    });
    expect(preparing).toMatchObject({
      status: "preparing",
      continuation: { jobId: ids.job, renderPlanDigest },
    });
    if (!preparing.continuation) throw new Error("fixture continuation is missing");
    await expect(
      viewer.continueSequencePreview(durableID(ids.project), durableID(ids.sequence), {
        expectedSequenceRevision: revisionString("2"),
        continuation: preparing.continuation,
      }),
    ).resolves.toMatchObject({
      status: "ready",
      continuation: { jobId: ids.job, renderPlanDigest },
      lease: { artifactId: ids.artifact, renderPlanDigest },
    });
  });

  it("keeps explicit sequence retry and closed recovery actions in the Creator port", async () => {
    const renderPlanDigest = `sha256:${"c".repeat(64)}`;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        expect(JSON.parse(String(init?.body))).toEqual({
          purpose: "sequence-preview",
          operation: "retry",
          expectedSequenceRevision: "2",
          continuation: { jobId: ids.job, renderPlanDigest },
        });
        return jsonResponse({
          status: "preparing",
          purpose: "sequence-preview",
          projectId: ids.project,
          sequenceId: ids.sequence,
          sequenceRevision: "2",
          stage: "render",
          diagnostics: [
            {
              code: "sequence-preview-integrity-rejected",
              severity: "blocking",
              subjectKind: "artifact",
              subjectId: ids.artifact,
              recovery: "automatic-retry",
            },
          ],
          continuation: { jobId: ids.retryJob, renderPlanDigest },
          job: {
            id: ids.retryJob,
            kind: "sequence-preview",
            state: "queued",
            progressBasisPoints: 0,
            renderPlanDigest,
            createdAt: "2026-07-15T09:02:00Z",
            updatedAt: "2026-07-15T09:02:00Z",
          },
        });
      }),
    );
    await expect(
      createContracts().media.viewer.retrySequencePreview(durableID(ids.project), durableID(ids.sequence), {
        expectedSequenceRevision: revisionString("2"),
        continuation: {
          jobId: durableID(ids.job),
          renderPlanDigest: digestString(renderPlanDigest),
        },
      }),
    ).resolves.toMatchObject({
      status: "preparing",
      continuation: { jobId: ids.retryJob, renderPlanDigest },
      diagnostics: [{ recovery: "automatic-retry" }],
    });
  });
});

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}
