import { afterEach, describe, expect, it, vi } from "vitest";

import { createContracts, isProductFeatureAvailable } from "../src/index.js";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("product feature availability", () => {
  it("adapts the closed semantic snapshot without exposing generated DTOs", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        response({
          schema: "open-cut/product-status/v1",
          features: [
            { feature: "asset-frame-inspection", state: "available" },
            { feature: "sequence-preview", state: "unavailable", reason: "not-qualified" },
            { feature: "sequence-export", state: "available" },
            { feature: "source-preview", state: "available" },
            { feature: "local-transcription", state: "unavailable", reason: "not-installed" },
          ],
        }),
      ),
    );
    const snapshot = await createContracts().product.read();
    expect(snapshot).toEqual({
      schema: "open-cut/product-status/v1",
      features: [
        { feature: "asset-frame-inspection", state: "available" },
        { feature: "sequence-preview", state: "unavailable", reason: "not-qualified" },
        { feature: "sequence-export", state: "available" },
        { feature: "source-preview", state: "available" },
        { feature: "local-transcription", state: "unavailable", reason: "not-installed" },
      ],
    });
    expect(isProductFeatureAvailable(snapshot, "source-preview")).toBe(true);
    expect(isProductFeatureAvailable(snapshot, "sequence-preview")).toBe(false);
  });

  it("rejects reordered, ambiguous, or open-ended feature state", async () => {
    for (const features of [
      [
        { feature: "sequence-preview", state: "available" },
        { feature: "asset-frame-inspection", state: "available" },
        { feature: "sequence-export", state: "available" },
        { feature: "source-preview", state: "available" },
        { feature: "local-transcription", state: "available" },
      ],
      [
        { feature: "asset-frame-inspection", state: "available", reason: "not-installed" },
        { feature: "sequence-preview", state: "available" },
        { feature: "sequence-export", state: "available" },
        { feature: "source-preview", state: "available" },
        { feature: "local-transcription", state: "available" },
      ],
      [
        { feature: "asset-frame-inspection", state: "available" },
        { feature: "sequence-preview", state: "unavailable", reason: "later" },
        { feature: "sequence-export", state: "available" },
        { feature: "source-preview", state: "available" },
        { feature: "local-transcription", state: "available" },
      ],
    ]) {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => response({ schema: "open-cut/product-status/v1", features })),
      );
      await expect(createContracts().product.read()).rejects.toThrow(/product feature|available product/);
    }
  });
});

function response(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}
