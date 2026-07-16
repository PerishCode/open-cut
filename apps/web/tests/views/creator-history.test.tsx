// @vitest-environment jsdom

import { ContractsProvider, type CreatorEditCommit, createContracts, durableID } from "@open-cut/contracts";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
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
  it("owns the newest durable transaction Undo across pane and actor boundaries", async () => {
    const requests: Array<{ url: string; body?: string }> = [];
    const onCommitted = vi.fn(async (_receipt: CreatorEditCommit) => undefined);
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => "018f0a60-7b80-7a01-8000-000000000908") });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        requests.push({ url, ...(init?.body ? { body: String(init.body) } : {}) });
        if (url.includes("/creator-edit/transactions")) return jsonResponse(history());
        if (url.endsWith(`/transactions/${ids.transaction}/undo`)) return jsonResponse(undoReceipt());
        throw new Error(`unexpected request ${url}`);
      }),
    );
    const base = createContracts();
    render(
      <ContractsProvider contracts={{ ...base, start: () => undefined, close: () => undefined }}>
        <CreatorHistory
          onCommitted={onCommitted}
          projectId={durableID(ids.project)}
          refreshEpoch={0}
          sequenceId={durableID(ids.sequence)}
        />
      </ContractsProvider>,
    );

    expect(await screen.findByText("Move selected Timeline Clip")).toBeTruthy();
    expect(screen.getByText(/LATEST · r8 · AGENT/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Undo latest change" }));

    await waitFor(() => expect(onCommitted).toHaveBeenCalledOnce());
    const undoRequest = requests.find((request) => request.url.endsWith(`/transactions/${ids.transaction}/undo`));
    expect(JSON.parse(undoRequest?.body ?? "{}")).toEqual({
      requestId: "ui:creator-history-undo:018f0a60-7b80-7a01-8000-000000000908",
      intent: "Undo latest creative change",
    });
    expect(onCommitted.mock.calls[0]?.[0]).toMatchObject({
      transactionId: ids.undo,
      undoesTransactionId: ids.transaction,
    });
  });

  it("presents undo-of-undo as Redo without a second history model", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse(history(true))),
    );
    const base = createContracts();
    render(
      <ContractsProvider contracts={{ ...base, start: () => undefined, close: () => undefined }}>
        <CreatorHistory
          onCommitted={async () => undefined}
          projectId={durableID(ids.project)}
          refreshEpoch={0}
          sequenceId={durableID(ids.sequence)}
        />
      </ContractsProvider>,
    );

    expect(await screen.findByRole("button", { name: "Redo previous change" })).toBeTruthy();
    expect(screen.getByText(/UNDO\/REDO/)).toBeTruthy();
  });
});

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

function undoReceipt() {
  return {
    proposal: {
      id: ids.proposal,
      projectId: ids.project,
      sequenceId: ids.sequence,
      requestId: "ui:creator-history-undo:018f0a60-7b80-7a01-8000-000000000908",
      actor: { kind: "creator", creatorId: ids.creator },
      status: "applied",
      appliedTransactionId: ids.undo,
      allocation: [],
    },
    transaction: {
      id: ids.undo,
      proposalId: ids.proposal,
      projectId: ids.project,
      actor: { kind: "creator", creatorId: ids.creator },
      intent: "Undo latest creative change",
      committedProjectRevision: "9",
      changes: [{ kind: "clip", id: ids.clip, before: "4", after: "5" }],
      undoesTransactionId: ids.transaction,
    },
    activityCursor: "15",
    replayed: false,
  };
}

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}
