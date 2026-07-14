import { describe, expect, it } from "vitest";

import { getHealth } from "../src/index.js";

describe("generated OpenAPI client", () => {
  it("keeps the manually selected API ingress", async () => {
    const originalFetch = globalThis.fetch;
    let requested: RequestInfo | URL | undefined;
    globalThis.fetch = async (input) => {
      requested = input;
      return new Response(JSON.stringify({ ok: true, service: "api" }), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    };
    try {
      await getHealth();
      expect(requested).toBe("/api/v1/health");
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});
