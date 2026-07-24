// @vitest-environment jsdom

import { ContractsProvider, createContracts, durableID, revisionString } from "@open-cut/contracts";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { CreatorVersions } from "../../src/components/creator-versions.js";

const ids = {
  project: "018f0a60-7b80-7a01-8000-000000000a01",
  version: "018f0a60-7b80-7a01-8000-000000000a02",
  current: "018f0a60-7b80-7a01-8000-000000000a03",
  safety: "018f0a60-7b80-7a01-8000-000000000a04",
  transaction: "018f0a60-7b80-7a01-8000-000000000a05",
  request: "018f0a60-7b80-7a01-8000-000000000a06",
} as const;

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("CreatorVersions", () => {
  it("creates a named lightweight checkpoint from the Versions panel", async () => {
    const requests: RequestInit[] = [];
    const entries = [version(ids.current, "8", "genesis", "Project created")];
    vi.stubGlobal("crypto", { randomUUID: () => ids.request });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        if (init?.method === "POST") {
          requests.push(init);
          const saved = version(ids.version, "8", "manual", "Approved assembly");
          entries.unshift(saved);
          return jsonResponse({ version: saved, activityCursor: "12", replayed: false });
        }
        return jsonResponse({ versions: entries, activityCursor: "11" });
      }),
    );

    renderVersions();
    expect(screen.getByText("Lightweight before Agent turns · named versions never copy Source media.")).toBeTruthy();
    expect(await screen.findByText("Project created")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Version name"), { target: { value: " Approved assembly " } });
    fireEvent.click(screen.getByRole("button", { name: "Save version" }));

    expect(await screen.findByText("Saved “Approved assembly” at r8.")).toBeTruthy();
    expect(requests).toHaveLength(1);
    expect(JSON.parse(String(requests[0]?.body))).toEqual({
      requestId: `ui:project-version-create:${ids.request}`,
      name: "Approved assembly",
    });
  });

  it("requires review and describes the automatic safety checkpoint before restore", async () => {
    const onRestored = vi.fn(async () => undefined);
    let restoreBody: unknown;
    const entries = [
      version(ids.current, "8", "agent-turn", "Before Agent turn"),
      version(ids.version, "5", "manual", "Approved assembly"),
    ];
    vi.stubGlobal("crypto", { randomUUID: () => ids.request });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith(`/${ids.version}/restore`)) {
          restoreBody = JSON.parse(String(init?.body));
          const safety = version(ids.safety, "8", "pre-restore", "Before restore");
          entries.unshift(safety);
          return jsonResponse({
            version: entries[2],
            safetyVersion: safety,
            transactionId: ids.transaction,
            committedProjectRevision: "9",
            activityCursor: "13",
            replayed: false,
          });
        }
        return jsonResponse({ versions: entries, activityCursor: "12" });
      }),
    );

    renderVersions(onRestored);
    expect(await screen.findByRole("button", { name: "Review restore" })).toBeTruthy();
    expect((screen.getByRole("button", { name: "Current version" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Review restore" }));
    expect(screen.getByText(/current state is checkpointed automatically first/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Restore as new revision" }));

    await waitFor(() => expect(onRestored).toHaveBeenCalledOnce());
    expect(restoreBody).toEqual({
      requestId: `ui:project-version-restore:${ids.request}`,
      expectedProjectRevision: "8",
    });
    expect(await screen.findByText(/Restored “Approved assembly” as r9/)).toBeTruthy();
  });
});

function renderVersions(onRestored = async () => undefined) {
  const base = createContracts();
  return render(
    <ContractsProvider contracts={{ ...base, start: () => undefined, close: () => undefined }}>
      <CreatorVersions
        currentRevision={revisionString("8")}
        onRestored={onRestored}
        projectId={durableID(ids.project)}
        refreshEpoch={0}
      />
    </ContractsProvider>,
  );
}

function version(id: string, revision: string, source: string, name: string) {
  return {
    id,
    projectId: ids.project,
    capturedProjectRevision: revision,
    source,
    name,
    digest: `sha256:${"a".repeat(64)}`,
    byteSize: "2048",
    retention: source === "manual" ? "manual" : "automatic",
    createdAt: "2026-07-22T04:00:00Z",
  };
}

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}
