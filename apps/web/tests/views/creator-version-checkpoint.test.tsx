// @vitest-environment jsdom

import { ContractsProvider, createContracts, durableID } from "@open-cut/contracts";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { CreatorVersionCheckpoint } from "../../src/components/creator-version-checkpoint.js";

const ids = {
  project: "018f0a60-7b80-7a01-8000-000000000b01",
  version: "018f0a60-7b80-7a01-8000-000000000b02",
  request: "018f0a60-7b80-7a01-8000-000000000b03",
} as const;

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("CreatorVersionCheckpoint", () => {
  it("creates a named lightweight checkpoint and reports the save", async () => {
    const requests: RequestInit[] = [];
    const onSaved = vi.fn(async () => undefined);
    vi.stubGlobal("crypto", { randomUUID: () => ids.request });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        requests.push(init ?? {});
        return jsonResponse({
          version: {
            id: ids.version,
            projectId: ids.project,
            capturedProjectRevision: "8",
            source: "manual",
            name: "Approved assembly",
            digest: `sha256:${"a".repeat(64)}`,
            byteSize: "2048",
            retention: "manual",
            createdAt: "2026-07-22T04:00:00Z",
          },
          activityCursor: "12",
          replayed: false,
        });
      }),
    );

    renderCheckpoint(onSaved);
    expect(screen.getByRole("region", { name: "Save named project version" })).toBeTruthy();
    expect(screen.getByText("AUTO BEFORE AGENT TURNS · SOURCE MEDIA STAYS SHARED")).toBeTruthy();
    const save = screen.getByRole("button", { name: "Save version" });
    expect((save as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("Version name"), { target: { value: " Approved assembly " } });
    fireEvent.click(save);

    expect(await screen.findByText("Saved “Approved assembly” at r8.")).toBeTruthy();
    expect(onSaved).toHaveBeenCalledOnce();
    expect(requests).toHaveLength(1);
    expect(JSON.parse(String(requests[0]?.body))).toEqual({
      requestId: `ui:project-version-create:${ids.request}`,
      name: "Approved assembly",
    });
  });

  it("keeps save failures private and recoverable", async () => {
    const onSaved = vi.fn(async () => undefined);
    vi.stubGlobal("crypto", { randomUUID: () => ids.request });
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new Error("sqlite: unable to open /Users/editor/Library/Application Support/Open Cut/project.db");
      }),
    );

    renderCheckpoint(onSaved);
    fireEvent.change(screen.getByLabelText("Version name"), { target: { value: "Approved assembly" } });
    fireEvent.click(screen.getByRole("button", { name: "Save version" }));

    expect(await screen.findByText("Could not save this project version. Try again.")).toBeTruthy();
    expect(screen.queryByText(/sqlite|Application Support|project\.db/i)).toBeNull();
    expect(onSaved).not.toHaveBeenCalled();
    expect((screen.getByRole("button", { name: "Save version" }) as HTMLButtonElement).disabled).toBe(false);
  });
});

function renderCheckpoint(onSaved: () => Promise<undefined>) {
  const base = createContracts();
  return render(
    <ContractsProvider contracts={{ ...base, start: () => undefined, close: () => undefined }}>
      <CreatorVersionCheckpoint onSaved={onSaved} projectId={durableID(ids.project)} />
    </ContractsProvider>,
  );
}

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}
