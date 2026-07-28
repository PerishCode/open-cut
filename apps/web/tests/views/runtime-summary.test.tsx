// @vitest-environment jsdom

import { ContractsProvider, createContracts } from "@open-cut/contracts";
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
    const current = { id: "018f0a60-7b80-7a01-8000-000000000b02", name: "Second story" };
    const projects = [{ id: "018f0a60-7b80-7a01-8000-000000000b01", name: "First story" }, current];
    const snapshot = { projects };
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
