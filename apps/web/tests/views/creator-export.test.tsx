// @vitest-environment jsdom

import { ContractsProvider, durableID, revisionString } from "@open-cut/contracts";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { CreatorExport } from "../../src/components/creator-export.js";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("CreatorExport", () => {
  it("rediscovers ready history and requires a second gesture before durable deletion", async () => {
    const projectId = durableID("018f0a60-7b80-7a01-8000-000000000201");
    const sequenceId = durableID("018f0a60-7b80-7a01-8000-000000000202");
    const jobId = "018f0a60-7b80-7a01-8000-000000000203";
    const artifactId = "018f0a60-7b80-7a01-8000-000000000204";
    let deleted = false;
    let deleteRequests = 0;
    let revealRequests = 0;
    vi.stubGlobal("crypto", { randomUUID: () => "018f0a60-7b80-7a01-8000-000000000205" });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url === `/api/v1/projects/${projectId}/exports?limit=20`) {
          return jsonResponse({
            lineages: [historyLineage(projectId, sequenceId, jobId, artifactId, deleted)],
            activityCursor: deleted ? "10" : "9",
          });
        }
        if (url === `/api/v1/projects/${projectId}/exports/${jobId}/artifact/delete`) {
          deleteRequests += 1;
          expect(init?.method).toBe("POST");
          expect(JSON.parse(String(init?.body))).toEqual({
            artifactId,
            requestId: "ui:export-delete:018f0a60-7b80-7a01-8000-000000000205",
          });
          deleted = true;
          return jsonResponse(exportResult(projectId, sequenceId, jobId, artifactId, true));
        }
        if (url === "/_open-cut/platform/export-save-as") {
          expect(JSON.parse(String(init?.body))).toEqual({
            projectId,
            artifactId,
            suggestedName: "History-story-r7.webm",
          });
          return jsonResponse({
            status: "saved",
            displayName: "History-story-r7.webm",
            byteLength: "100",
            contentSha256: `sha256:${"a".repeat(64)}`,
            deliveryReceipt: `delivery.${"B".repeat(32)}`,
          });
        }
        if (url === "/_open-cut/platform/export-reveal") {
          revealRequests += 1;
          expect(JSON.parse(String(init?.body))).toEqual({ deliveryReceipt: `delivery.${"B".repeat(32)}` });
          return jsonResponse({ status: "revealed", displayName: "History-story-r7.webm" });
        }
        if (url === `/api/v1/projects/${projectId}/events?after=9`) return eventStream(init?.signal);
        throw new Error(`unexpected request ${init?.method ?? "GET"} ${url}`);
      }),
    );

    const view = render(
      <ContractsProvider>
        <CreatorExport
          available
          hasContent
          projectId={projectId}
          projectName="History story"
          sequenceId={sequenceId}
          sequenceRevision={revisionString("7")}
        />
      </ContractsProvider>,
    );
    const saveAsButton = await screen.findByRole("button", {
      name: "Save export History-story-r7.webm, history item 1, from 2026-07-16 00:00 UTC as",
    });
    expect(screen.getByText("DESTINATION AFTER RENDER · WEBM · VP9 / OPUS")).toBeTruthy();
    expect(screen.getAllByText("Ready").length).toBe(2);
    fireEvent.click(saveAsButton);
    expect(await screen.findByText(/Saved History-story-r7\.webm/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Reveal saved export History-story-r7.webm in folder" }));
    expect(await screen.findByText("Revealed History-story-r7.webm")).toBeTruthy();
    expect(revealRequests).toBe(1);
    fireEvent.click(
      screen.getByRole("button", {
        name: "Delete export History-story-r7.webm, history item 1, from 2026-07-16 00:00 UTC",
      }),
    );
    expect(deleteRequests).toBe(0);
    expect(screen.getByText("This removes the exported media but keeps its job history.")).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", {
        name: "Delete export History-story-r7.webm, history item 1, from 2026-07-16 00:00 UTC permanently",
      }),
    );
    expect(await screen.findByText("Media deleted")).toBeTruthy();
    expect(
      screen.getByRole("button", {
        name: "Retry export History-story-r7.webm, history item 1, from 2026-07-16 00:00 UTC",
      }),
    ).toBeTruthy();
    expect(deleteRequests).toBe(1);
    view.unmount();
  });

  it("keeps running export actions mutually exclusive and never projects undefined as delete confirmation", async () => {
    const projectId = durableID("018f0a60-7b80-7a01-8000-000000000221");
    const sequenceId = durableID("018f0a60-7b80-7a01-8000-000000000222");
    const jobId = durableID("018f0a60-7b80-7a01-8000-000000000223");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url === `/api/v1/projects/${projectId}/exports?limit=20`) {
          return jsonResponse({
            lineages: [activeHistoryLineage(projectId, sequenceId, jobId)],
            activityCursor: "11",
          });
        }
        if (url === `/api/v1/projects/${projectId}/events?after=11`) return eventStream(init?.signal);
        throw new Error(`unexpected request ${init?.method ?? "GET"} ${url}`);
      }),
    );

    render(
      <ContractsProvider>
        <CreatorExport
          available
          hasContent
          projectId={projectId}
          projectName="Running story"
          sequenceId={sequenceId}
          sequenceRevision={revisionString("8")}
        />
      </ContractsProvider>,
    );

    expect(await screen.findByText("Rendering")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Export in progress" }) as HTMLButtonElement).disabled).toBe(true);
    expect(
      screen.getByRole("button", {
        name: "Cancel export Running-story-r8.webm, history item 1, from 2026-07-16 00:00 UTC",
      }),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Save export/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Delete export .* permanently/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Keep export/ })).toBeNull();
    expect(screen.queryByText("This removes the exported media but keeps its job history.")).toBeNull();
  });

  it("explains an empty Sequence before offering an invalid export", async () => {
    const projectId = durableID("018f0a60-7b80-7a01-8000-000000000211");
    const sequenceId = durableID("018f0a60-7b80-7a01-8000-000000000212");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url === `/api/v1/projects/${projectId}/exports?limit=20`) {
          return jsonResponse({ lineages: [], activityCursor: "1" });
        }
        if (url === `/api/v1/projects/${projectId}/events?after=1`) return eventStream(init?.signal);
        throw new Error(`unexpected request ${init?.method ?? "GET"} ${url}`);
      }),
    );

    render(
      <ContractsProvider>
        <CreatorExport
          available
          hasContent={false}
          projectId={projectId}
          projectName="Empty story"
          sequenceId={sequenceId}
          sequenceRevision={revisionString("1")}
        />
      </ContractsProvider>,
    );

    const exportButton = await screen.findByRole("button", { name: "Nothing to export" });
    expect((exportButton as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText("Add a clip or caption to the Sequence before exporting.")).toBeTruthy();
  });
});

function historyLineage(projectId: string, sequenceId: string, jobId: string, artifactId: string, deleted: boolean) {
  return {
    origin: "creator",
    attemptCount: "1",
    artifactAvailability: deleted ? "deleted" : "ready",
    rootCreatedAt: "2026-07-16T00:00:00Z",
    export: exportResult(projectId, sequenceId, jobId, artifactId, deleted),
  };
}

function exportResult(projectId: string, sequenceId: string, jobId: string, artifactId: string, deleted: boolean) {
  return {
    projectId,
    sequenceId,
    sequenceRevision: "7",
    preset: "webm-vp9-opus-v1",
    job: {
      id: jobId,
      rootJobId: jobId,
      state: "succeeded",
      progressBasisPoints: 10_000,
      createdAt: "2026-07-16T00:00:00Z",
      updatedAt: "2026-07-16T00:00:01Z",
    },
    ...(!deleted
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
    recovery: deleted ? "retry-job" : "none",
    replayed: false,
    activityCursor: deleted ? "10" : "9",
  };
}

function activeHistoryLineage(projectId: string, sequenceId: string, jobId: string) {
  return {
    origin: "creator",
    attemptCount: "1",
    artifactAvailability: "none",
    rootCreatedAt: "2026-07-16T00:00:00Z",
    export: {
      projectId,
      sequenceId,
      sequenceRevision: "8",
      preset: "webm-vp9-opus-v1",
      job: {
        id: jobId,
        rootJobId: jobId,
        state: "running",
        progressBasisPoints: 2_500,
        createdAt: "2026-07-16T00:00:00Z",
        updatedAt: "2026-07-16T00:00:01Z",
      },
      recovery: "none",
      replayed: false,
      activityCursor: "11",
    },
  };
}

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}

function eventStream(signal?: AbortSignal | null): Response {
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      signal?.addEventListener("abort", () => controller.close(), { once: true });
    },
  });
  return new Response(body, { status: 200, headers: { "content-type": "text/event-stream" } });
}
