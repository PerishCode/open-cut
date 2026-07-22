import { afterEach, describe, expect, it, vi } from "vitest";

import { createContracts, durableID, revisionString } from "../src/index.js";
import { jsonResponse } from "./http-fixtures.js";
import { testIDs as ids } from "./test-identities.js";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("Project version Contracts", () => {
  it("lists, creates, and restores exact project checkpoints", async () => {
    const version = projectVersion(ids.proposal, "5", "manual", "Assembly checkpoint");
    const safety = projectVersion(ids.undoTransaction, "8", "pre-restore", "Before restore");
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValueOnce(jsonResponse({ versions: [version], activityCursor: "21" }))
        .mockResolvedValueOnce(jsonResponse({ version, activityCursor: "22", replayed: false }))
        .mockResolvedValueOnce(
          jsonResponse({
            version,
            safetyVersion: safety,
            transactionId: ids.transaction,
            committedProjectRevision: "9",
            activityCursor: "23",
            replayed: false,
          }),
        ),
    );

    const contracts = createContracts();
    const projectId = durableID(ids.alpha);
    const page = await contracts.projects.versions.list(projectId, { limit: 20 });
    expect(page.versions[0]).toMatchObject({ id: ids.proposal, capturedProjectRevision: "5", source: "manual" });

    await contracts.projects.versions.create({
      projectId,
      requestId: "ui:version:create:1",
      name: " Assembly checkpoint ",
    });
    const restored = await contracts.projects.versions.restore({
      projectId,
      versionId: durableID(ids.proposal),
      requestId: "ui:version:restore:1",
      expectedProjectRevision: revisionString("8"),
    });

    expect(restored).toMatchObject({
      committedProjectRevision: "9",
      transactionId: ids.transaction,
      safetyVersion: { id: ids.undoTransaction, source: "pre-restore" },
    });
    expect(vi.mocked(fetch).mock.calls[1]?.[1]?.body).toBe(
      JSON.stringify({ requestId: "ui:version:create:1", name: "Assembly checkpoint" }),
    );
    expect(vi.mocked(fetch).mock.calls[2]?.[1]?.body).toBe(
      JSON.stringify({ requestId: "ui:version:restore:1", expectedProjectRevision: "8" }),
    );
  });

  it("rejects cross-project and incomplete trigger payloads", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse({
          versions: [{ ...projectVersion(ids.proposal, "5", "agent-turn", "Before Agent turn"), triggerKind: "turn" }],
          activityCursor: "21",
        }),
      ),
    );

    await expect(createContracts().projects.versions.list(durableID(ids.alpha))).rejects.toThrow(
      "trigger is incomplete",
    );
  });
});

function projectVersion(id: string, revision: string, source: string, name: string) {
  return {
    id,
    projectId: ids.alpha,
    capturedProjectRevision: revision,
    source,
    name,
    digest: `sha256:${"a".repeat(64)}`,
    byteSize: "1024",
    retention: source === "manual" ? "manual" : "automatic",
    createdAt: "2026-07-22T04:00:00Z",
  };
}
