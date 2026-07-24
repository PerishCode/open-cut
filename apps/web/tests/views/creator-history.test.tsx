// @vitest-environment jsdom

import { ContractsProvider, createContracts, durableID } from "@open-cut/contracts";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { CreatorHistory } from "../../src/components/creator-history.js";

const ids = {
  project: "018f0a60-7b80-7a01-8000-000000000901",
  sequence: "018f0a60-7b80-7a01-8000-000000000902",
  clip: "018f0a60-7b80-7a01-8000-000000000903",
  creator: "018f0a60-7b80-7a01-8000-000000000904",
  proposal: "018f0a60-7b80-7a01-8000-000000000905",
  transaction: "018f0a60-7b80-7a01-8000-000000000906",
  undo: "018f0a60-7b80-7a01-8000-000000000907",
} as const;

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("Creator Workspace history", () => {
  it("keeps history storage failures private and retries the log", async () => {
    let attempts = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        attempts += 1;
        if (attempts === 1) {
          throw new Error("sqlite: malformed database at /Users/editor/.open-cut/project.db");
        }
        return jsonResponse(history());
      }),
    );
    renderHistory();

    expect(await screen.findByText("Could not load project history.")).toBeTruthy();
    expect(screen.queryByText(/sqlite|\/Users\/editor|project\.db/i)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));

    expect(await screen.findByText(/Move selected Timeline Clip/)).toBeTruthy();
    expect(attempts).toBe(2);
  });

  it("presents the durable transaction log as read-only technical detail", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request) => {
        const url = String(input);
        if (url.includes("/creator-edit/transactions")) return jsonResponse(history());
        throw new Error(`unexpected request ${url}`);
      }),
    );
    renderHistory();

    expect(await screen.findByText(/Move selected Timeline Clip/)).toBeTruthy();
    expect(screen.getByText(/LATEST · r8 · AGENT/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Undo|Redo/ })).toBeNull();
  });

  it("retains prior undo transactions as labeled audit records", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse(history(true))),
    );
    renderHistory();

    expect(await screen.findByText(/UNDO\/REDO/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Undo|Redo/ })).toBeNull();
  });
});

function renderHistory() {
  const base = createContracts();
  return render(
    <ContractsProvider contracts={{ ...base, start: () => undefined, close: () => undefined }}>
      <CreatorHistory projectId={durableID(ids.project)} refreshEpoch={0} />
    </ContractsProvider>,
  );
}

function history(undone = false) {
  return {
    transactions: [
      {
        id: undone ? ids.undo : ids.transaction,
        intent: undone ? "Undo latest creative change" : "Move selected Timeline Clip",
        actor: undone ? "creator" : "agent",
        committedProjectRevision: undone ? "9" : "8",
        changes: [{ kind: "clip", id: ids.clip, before: undone ? "4" : "3", after: undone ? "5" : "4" }],
        ...(undone ? { undoesTransactionId: ids.transaction } : {}),
        committedAt: "2026-07-16T04:00:00Z",
      },
    ],
    activityCursor: "14",
  };
}

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}
