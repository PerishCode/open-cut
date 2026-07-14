// @vitest-environment jsdom

import { ContractsProvider } from "@open-cut/contracts";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { HomeView } from "../../src/views/home-view.js";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("HomeView", () => {
  it("renders project state through Contracts hooks", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url === "/api/v1/projects") {
          return jsonResponse({
            revision: 7,
            projects: [{ id: "alpha", name: "Alpha", description: "First project" }],
          });
        }
        if (url === "/api/v1/events") return eventStream(init?.signal);
        throw new Error(["unexpected request", init?.method ?? "GET", url].join(" "));
      }),
    );
    const view = render(
      <ContractsProvider>
        <HomeView />
      </ContractsProvider>,
    );
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Peer sidecars, one control plane.");
    expect(await screen.findByText("Projects synchronized at revision 7")).toBeTruthy();
    expect(screen.getByText("Projects: Alpha")).toBeTruthy();
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/projects",
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    view.unmount();
  });
});

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
