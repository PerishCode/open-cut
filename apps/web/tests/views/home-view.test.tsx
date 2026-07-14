// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { HomeView } from "../../src/views/home-view.js";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("HomeView", () => {
  it("renders through shared atoms and consumes the generated API client", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Promise.resolve(
          new Response(JSON.stringify({ ok: true, service: "api" }), {
            status: 200,
            headers: { "content-type": "application/json" },
          }),
        ),
      ),
    );
    render(<HomeView />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Peer sidecars, one control plane.");
    expect(await screen.findByText("API runtime ready")).toBeTruthy();
    expect(fetch).toHaveBeenCalledWith("/api/v1/health", expect.objectContaining({ signal: expect.any(AbortSignal) }));
  });
});
