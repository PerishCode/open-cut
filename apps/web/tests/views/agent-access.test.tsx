// @vitest-environment jsdom

import { ContractsProvider } from "@open-cut/contracts";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AgentAccess } from "../../src/components/agent-access.js";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("AgentAccess", () => {
  it("keeps local CLI failures private and reloads authoritative access", async () => {
    let attempts = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        attempts += 1;
        if (attempts === 1) {
          throw new Error("spawn /Users/editor/.local/bin/open-cut: permission denied");
        }
        return jsonResponse({ grants: [], upgrades: [] });
      }),
    );

    render(
      <ContractsProvider>
        <AgentAccess />
      </ContractsProvider>,
    );

    expect(await screen.findByText("Could not update CLI access.")).toBeTruthy();
    expect(screen.queryByText(/\/Users\/|permission denied|spawn/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Check again" }));

    await waitFor(() => expect(screen.queryByText("Could not update CLI access.")).toBeNull());
    expect(attempts).toBeGreaterThanOrEqual(2);
  });

  it("hides protocol identity and fails closed on permissions this UI cannot explain", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request) => {
        if (String(input) !== "/api/v1/authorization/cli/pairings") {
          throw new Error(`unexpected request ${String(input)}`);
        }
        return jsonResponse({
          grants: [
            {
              id: "018f0a60-7b80-7f01-8000-000000000101",
              installationId: "installation:test",
              agentId: "018f0a60-7b80-7f01-8000-000000000102",
              publicKeyFingerprint: `sha256:${"a".repeat(64)}`,
              scopes: ["project:read", "future:control"],
              revision: "1",
              scopeDigest: `sha256:${"b".repeat(64)}`,
              status: "pending",
              createdAt: "2026-07-24T03:00:00Z",
              expiresAt: "2026-07-24T03:10:00Z",
            },
          ],
          upgrades: [],
        });
      }),
    );

    render(
      <ContractsProvider>
        <AgentAccess />
      </ContractsProvider>,
    );

    expect(await screen.findByText("New CLI access request")).toBeTruthy();
    expect(screen.getByText("Can view projects · 1 additional permission needs review")).toBeTruthy();
    expect(screen.getByText("Update Open Cut before reviewing additional permissions.")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Approve CLI" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Deny CLI" }) as HTMLButtonElement).disabled).toBe(false);
    expect(screen.queryByText(/sha256:|future:control|installation:test/)).toBeNull();
  });
});

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}
