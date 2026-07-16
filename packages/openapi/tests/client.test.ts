import { readFile } from "node:fs/promises";
import { describe, expect, it } from "vitest";

import { getHealth } from "../src/generated/health.js";

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

  it("keeps Creator export history on the safe ExportData projection", async () => {
    const spec = JSON.parse(await readFile(new URL("../spec/openapi.json", import.meta.url), "utf8")) as {
      components: { schemas: Record<string, unknown> };
    };
    const lineage = spec.components.schemas.ExportLineageData;
    expect(lineage).toMatchObject({
      properties: {
        export: { $ref: "#/components/schemas/ExportData" },
      },
    });
    expect(JSON.stringify(lineage)).not.toMatch(/renderer|renderPlan|producerJob|byteReference/);
  });
});
