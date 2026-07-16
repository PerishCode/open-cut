import { afterEach, describe, expect, it, vi } from "vitest";

import { createContracts, durableID } from "../src/index.js";
import { jsonResponse } from "./http-fixtures.js";
import { testIDs as ids } from "./test-identities.js";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("Creator history Contracts", () => {
  it("exposes newest-first semantic transactions without journal operation payloads", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse({
          transactions: [
            {
              id: ids.undoTransaction,
              intent: "Undo selected Timeline Clip move",
              actor: "creator",
              committedProjectRevision: "9",
              changes: [{ kind: "clip", id: ids.clip, before: "4", after: "5" }],
              undoesTransactionId: ids.transaction,
              committedAt: "2026-07-16T04:00:01.000000000Z",
            },
            {
              id: ids.transaction,
              intent: "Move selected Timeline Clip",
              actor: "agent",
              committedProjectRevision: "8",
              changes: [
                { kind: "clip", id: ids.clip, before: "3", after: "4" },
                { kind: "alignment", id: ids.clipAlignment, before: "2", after: "3" },
              ],
              committedAt: "2026-07-16T04:00:00Z",
            },
          ],
          nextBefore: "8",
          activityCursor: "14",
        }),
      ),
    );

    const page = await createContracts().editing.history.list({ projectId: durableID(ids.alpha), limit: 2 });

    expect(page).toEqual({
      transactions: [
        {
          id: ids.undoTransaction,
          intent: "Undo selected Timeline Clip move",
          actor: "creator",
          committedProjectRevision: "9",
          changes: [{ kind: "clip", id: ids.clip, beforeRevision: "4", revision: "5", tombstoned: false }],
          undoesTransactionId: ids.transaction,
          committedAt: "2026-07-16T04:00:01.000000000Z",
        },
        {
          id: ids.transaction,
          intent: "Move selected Timeline Clip",
          actor: "agent",
          committedProjectRevision: "8",
          changes: [
            { kind: "clip", id: ids.clip, beforeRevision: "3", revision: "4", tombstoned: false },
            {
              kind: "alignment",
              id: ids.clipAlignment,
              beforeRevision: "2",
              revision: "3",
              tombstoned: false,
            },
          ],
          committedAt: "2026-07-16T04:00:00Z",
        },
      ],
      nextBefore: "8",
      activityCursor: "14",
    });
    expect(vi.mocked(fetch).mock.calls[0]?.[0].toString()).toContain("limit=2");
  });

  it("rejects ascending or duplicated transaction revisions", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse({
          transactions: [transaction(ids.transaction, "8"), transaction(ids.undoTransaction, "9")],
          activityCursor: "14",
        }),
      ),
    );

    await expect(createContracts().editing.history.list({ projectId: durableID(ids.alpha) })).rejects.toThrow(
      "order is invalid",
    );
  });
});

function transaction(id: string, revision: string) {
  return {
    id,
    intent: "Timeline edit",
    actor: "creator",
    committedProjectRevision: revision,
    changes: [],
    committedAt: "2026-07-16T04:00:00Z",
  };
}
