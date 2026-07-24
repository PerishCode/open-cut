// @vitest-environment jsdom

import { ContractsProvider } from "@open-cut/contracts";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ProductResources } from "../../src/components/product-resources.js";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("ProductResources", () => {
  it("presents offline readiness without exposing the resource build identity", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request) => {
        if (String(input) !== "/api/v1/product/resources") {
          throw new Error(`unexpected request ${String(input)}`);
        }
        return jsonResponse({
          schema: "open-cut/product-resource-snapshot/v1",
          resources: [
            {
              name: "whisper-small",
              kind: "transcription-model",
              version: "whisper-small@c521a4b02f422512",
              profile: "multilingual",
              byteSize: "488636416",
              state: "ready",
              progressBasisPoints: 10000,
              resourceId: "018f0a60-7b80-7f01-8000-000000000101",
              jobId: "018f0a60-7b80-7f01-8000-000000000102",
              updatedAt: "2026-07-24T03:00:00Z",
            },
          ],
        });
      }),
    );

    render(
      <ContractsProvider>
        <ProductResources />
      </ContractsProvider>,
    );

    expect(await screen.findByText("Multilingual transcription")).toBeTruthy();
    expect(screen.getByText("Ready offline")).toBeTruthy();
    expect(screen.getByText("466 MiB")).toBeTruthy();
    expect(screen.queryByText(/whisper-small|c521a4b02f422512/)).toBeNull();
  });
});

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}
