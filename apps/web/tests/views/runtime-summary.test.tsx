// @vitest-environment jsdom

import {
  ContractsProvider,
  createContracts,
  cursorString,
  durableID,
  type Project,
  type ProjectState,
  revisionString,
} from "@open-cut/contracts";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { RuntimeSummary } from "../../src/components/runtime-summary.js";

describe("RuntimeSummary", () => {
  it("keeps project storage failures private and leaves create available", async () => {
    const base = createContracts();
    const create = vi.fn(async () => {
      throw new Error("sqlite write failed at /Users/editor/Library/Application Support/Open Cut/project.db");
    });
    const contracts = {
      ...base,
      projects: { ...base.projects, write: { create } },
      start: () => undefined,
      close: () => undefined,
    };
    render(
      <ContractsProvider contracts={contracts}>
        <RuntimeSummary />
      </ContractsProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Create and open" }));

    expect(await screen.findByText("Project could not be created. Review the name and try again.")).toBeTruthy();
    expect(screen.queryByText(/sqlite|Application Support|project\.db/i)).toBeNull();
    expect((screen.getByRole("button", { name: "Create and open" }) as HTMLButtonElement).disabled).toBe(false);
    expect(create).toHaveBeenCalledOnce();
  });

  it("marks the open project and offers an explicit return when reached from a workspace", () => {
    const base = createContracts();
    const project = (suffix: string, name: string): Project => ({
      id: durableID(`018f0a60-7b80-7a01-8000-00000000${suffix}`),
      revision: revisionString("1"),
      lifecycleRevision: revisionString("1"),
      name,
      status: "active",
      narrativeDocumentId: durableID(`018f0a60-7b80-7a01-8000-00000001${suffix}`),
      mainSequenceId: durableID(`018f0a60-7b80-7a01-8000-00000002${suffix}`),
    });
    const current = project("0b02", "Second story");
    const snapshot: ProjectState = {
      status: "ready",
      activityCursor: cursorString("0"),
      projects: [project("0b01", "First story"), current],
    };
    const contracts = {
      ...base,
      projects: {
        ...base.projects,
        read: {
          ...base.projects.read,
          subscribe: () => () => undefined,
          getSnapshot: () => snapshot,
        },
      },
      start: () => undefined,
      close: () => undefined,
    };
    const onReturn = vi.fn();
    render(
      <ContractsProvider contracts={contracts}>
        <RuntimeSummary
          currentProject={{ id: current.id, name: current.name }}
          onOpen={() => undefined}
          onReturn={onReturn}
        />
      </ContractsProvider>,
    );

    expect(screen.getByText("Open now")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Return to Second story" }));
    expect(onReturn).toHaveBeenCalledOnce();
  });
});
