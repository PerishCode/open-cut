import { afterEach, describe, expect, it, vi } from "vitest";

import { createContracts } from "../src/index.js";

const ids = {
  job: "018f0a60-7b80-7a01-8000-000000000001",
  resource: "018f0a60-7b80-7a01-8000-000000000002",
} as const;

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("product resource contracts", () => {
  it("adapts Creator acquisition without exposing catalog origins, paths, or digests", async () => {
    const ready = {
      name: "whisper-small-multilingual",
      kind: "transcription-model",
      version: "small-v3",
      profile: "whisper-small-multilingual-v1",
      byteSize: "466000000",
      state: "ready",
      resourceId: ids.resource,
      jobId: ids.job,
      progressBasisPoints: 10_000,
      updatedAt: "2026-07-15T01:02:03Z",
      origin: "https://must-not-leak.example/model.bin",
      sha256: "sha256:must-not-leak",
      byteReference: "/must/not/leak",
    };
    const fetchMock = vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/v1/product/resources") {
        return response({ schema: "open-cut/product-resource-snapshot/v1", resources: [ready] });
      }
      if (url === "/api/v1/product/resources/whisper-small-multilingual/acquisition") {
        expect(init?.method).toBe("POST");
        expect(init?.body).toBe(JSON.stringify({ requestId: "ui:resource:one" }));
        return response({ resource: ready, activityCursor: "8", replayed: false });
      }
      throw new Error(`unexpected request ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    const port = createContracts().resources;

    const snapshot = await port.list();
    expect(snapshot.resources[0]).toEqual({
      name: "whisper-small-multilingual",
      kind: "transcription-model",
      version: "small-v3",
      profile: "whisper-small-multilingual-v1",
      byteSize: "466000000",
      state: "ready",
      resourceId: ids.resource,
      jobId: ids.job,
      progressBasisPoints: 10_000,
      updatedAt: "2026-07-15T01:02:03Z",
    });
    expect(JSON.stringify(snapshot)).not.toMatch(/origin|sha256|byteReference|must-not-leak/);
    await expect(
      port.acquire({ name: "whisper-small-multilingual", requestId: "ui:resource:one" }),
    ).resolves.toMatchObject({ activityCursor: "8", replayed: false, resource: { state: "ready" } });
  });

  it("rejects non-canonical ordering and contradictory resource state", async () => {
    for (const resources of [
      [notAcquired("z-model"), notAcquired("a-model")],
      [{ ...notAcquired("a-model"), progressBasisPoints: 1 }],
      [{ ...notAcquired("a-model"), byteSize: 123 }],
      [{ ...notAcquired("a-model"), state: "future" }],
    ]) {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => response({ schema: "open-cut/product-resource-snapshot/v1", resources })),
      );
      await expect(createContracts().resources.list()).rejects.toThrow(/product resource|uint64/);
    }
  });
});

function notAcquired(name: string): Record<string, unknown> {
  return {
    name,
    kind: "transcription-model",
    version: "v1",
    profile: "whisper-small-multilingual-v1",
    byteSize: "466000000",
    state: "not-acquired",
    progressBasisPoints: 0,
  };
}

function response(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}
