import { afterEach, describe, expect, it, vi } from "vitest";

import { cursorString, durableID, revisionString } from "../src/exact.js";
import { createSequenceExportPort } from "../src/exports.js";

const projectId = durableID("018f0a60-7b80-7a01-8000-000000000201");
const sequenceId = durableID("018f0a60-7b80-7a01-8000-000000000202");
const jobId = "018f0a60-7b80-7a01-8000-000000000203";
const artifactId = durableID("018f0a60-7b80-7a01-8000-000000000204");

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Creator Sequence export contract", () => {
  it("normalizes exact state and starts an activity-driven project watch", async () => {
    let invalidate: (() => void) | undefined;
    let watchedAfter = "";
    let notifications = 0;
    const port = createSequenceExportPort((_projectId, after, listener) => {
      watchedAfter = after;
      invalidate = listener;
      return () => undefined;
    });
    port.subscribe(projectId, () => {
      notifications += 1;
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify(exportResult("running")), { status: 200 })),
    );

    const result = await port.start(projectId, sequenceId, {
      requestId: "ui:export:start:1",
      sequenceRevision: revisionString("7"),
      preset: "webm-vp9-opus-v1",
    });

    expect(result.job.id).toBe(jobId);
    expect(result.sequenceRevision).toBe("7");
    expect(watchedAfter).toBe(cursorString("9"));
    invalidate?.();
    expect(notifications).toBe(1);
    port.close();
  });

  it("keeps overwrite authority opaque and returns only a safe Save As receipt", async () => {
    const destinationGrant = `destination.${"A".repeat(32)}`;
    const deliveryReceipt = `delivery.${"B".repeat(32)}`;
    const requests: Record<string, unknown>[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        if (String(input) === "/_open-cut/platform/export-reveal") {
          requests.push(JSON.parse(String(init?.body)) as Record<string, unknown>);
          return new Response(JSON.stringify({ status: "revealed", displayName: "story.webm" }), { status: 200 });
        }
        requests.push(JSON.parse(String(init?.body)) as Record<string, unknown>);
        if (requests.length === 1) {
          return new Response(
            JSON.stringify({
              error: "OC_EXPORT_OVERWRITE_REQUIRED",
              destinationGrant,
              displayName: "story.webm",
            }),
            { status: 409 },
          );
        }
        return new Response(
          JSON.stringify({
            status: "saved",
            displayName: "story.webm",
            byteLength: "18",
            contentSha256: `sha256:${"b".repeat(64)}`,
            deliveryReceipt,
          }),
          { status: 200 },
        );
      }),
    );
    const port = createSequenceExportPort();
    const conflict = await port.saveAs({
      projectId,
      artifactId,
      suggestedName: "story.webm",
    });
    expect(conflict.status).toBe("overwrite-required");
    if (conflict.status !== "overwrite-required") throw new Error("expected overwrite conflict");
    const saved = await port.saveAs({
      projectId,
      artifactId,
      suggestedName: "story.webm",
      destinationGrant: conflict.destinationGrant,
      overwrite: true,
    });
    expect(saved.status).toBe("saved");
    expect(requests[1]).toEqual({
      projectId,
      artifactId,
      suggestedName: "story.webm",
      destinationGrant,
      overwrite: true,
    });
    expect(JSON.stringify(saved)).not.toContain("/");
    if (saved.status !== "saved") throw new Error("expected saved export");
    expect(await port.reveal(saved.deliveryReceipt)).toEqual({ status: "revealed", displayName: "story.webm" });
    expect(requests[2]).toEqual({ deliveryReceipt });
  });

  it("normalizes bounded lineage history and deletes only by exact opaque Creator mutation", async () => {
    const requests: Array<{ url: string; method: string; body?: unknown }> = [];
    const succeeded = exportResult("succeeded");
    const { artifact: _artifact, ...withoutArtifact } = succeeded;
    const deleted = { ...withoutArtifact, recovery: "retry-job", activityCursor: "10" };
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        requests.push({
          url,
          method: init?.method ?? "GET",
          ...(init?.body ? { body: JSON.parse(String(init.body)) } : {}),
        });
        if (url.endsWith("/exports?limit=1")) {
          return new Response(
            JSON.stringify({
              lineages: [
                {
                  origin: "agent",
                  attemptCount: "2",
                  artifactAvailability: "deleted",
                  rootCreatedAt: "2026-07-16T00:00:00Z",
                  export: deleted,
                },
              ],
              nextAfter: "export-root.v1.opaque",
              activityCursor: "10",
            }),
            { status: 200 },
          );
        }
        return new Response(JSON.stringify(deleted), { status: 200 });
      }),
    );
    let watchedAfter = "";
    const port = createSequenceExportPort((_projectId, after) => {
      watchedAfter = after;
      return () => undefined;
    });
    port.subscribe(projectId, () => undefined);

    const page = await port.list(projectId, { limit: 1 });
    expect(page.lineages[0]).toMatchObject({
      origin: "agent",
      attemptCount: "2",
      artifactAvailability: "deleted",
      rootCreatedAt: "2026-07-16T00:00:00Z",
    });
    expect(page.lineages[0]?.export.artifact).toBeUndefined();
    expect(watchedAfter).toBe("10");

    const result = await port.deleteArtifact(projectId, durableID(jobId), artifactId, "ui:delete:1");
    expect(result.recovery).toBe("retry-job");
    expect(requests[1]).toEqual({
      url: `/api/v1/projects/${projectId}/exports/${jobId}/artifact/delete`,
      method: "POST",
      body: { artifactId, requestId: "ui:delete:1" },
    });
    expect(JSON.stringify(page)).not.toContain("runId");
    port.close();
  });
});

function exportResult(state: "running" | "succeeded") {
  return {
    projectId,
    sequenceId,
    sequenceRevision: "7",
    preset: "webm-vp9-opus-v1",
    job: {
      id: jobId,
      rootJobId: jobId,
      state,
      progressBasisPoints: state === "running" ? 2500 : 10_000,
      createdAt: "2026-07-16T00:00:00Z",
      updatedAt: "2026-07-16T00:00:01Z",
    },
    ...(state === "succeeded"
      ? {
          artifact: {
            id: artifactId,
            verification: "passed",
            semanticDuration: { value: "1", scale: 1 },
            presentationDuration: { value: "1", scale: 1 },
            canvasWidth: 1920,
            canvasHeight: 1080,
            frameRate: { value: "30", scale: 1 },
            videoFrameCount: "30",
            audioSampleRate: 48_000,
            audioSampleCount: "48000",
            videoCodec: "vp9",
            audioCodec: "opus",
            pixelFormat: "yuv420p",
            channelLayout: "stereo",
            byteSize: "100",
            contentDigest: `sha256:${"a".repeat(64)}`,
          },
        }
      : {}),
    recovery: "none",
    replayed: false,
    activityCursor: "9",
  };
}
