// @vitest-environment jsdom

import {
  ContractsProvider,
  createContracts,
  durableID,
  type NarrativeSubtree,
  revisionString,
} from "@open-cut/contracts";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { CreatorNarrativeWriter } from "../../src/components/creator-narrative-writer.js";

const ids = {
  project: "018f0a60-7b80-7a01-8000-000000000501",
  sequence: "018f0a60-7b80-7a01-8000-000000000502",
  document: "018f0a60-7b80-7a01-8000-000000000503",
  root: "018f0a60-7b80-7a01-8000-000000000504",
  text: "018f0a60-7b80-7a01-8000-000000000505",
  text2: "018f0a60-7b80-7a01-8000-00000000050c",
  section: "018f0a60-7b80-7a01-8000-000000000511",
  proposal: "018f0a60-7b80-7a01-8000-000000000506",
  transaction: "018f0a60-7b80-7a01-8000-000000000507",
  creator: "018f0a60-7b80-7a01-8000-000000000508",
} as const;

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("CreatorNarrativeWriter", () => {
  it("keeps newer dirty text while one complete-value checkpoint is in flight", async () => {
    const response = deferred<Response>();
    const bodies: unknown[] = [];
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => "018f0a60-7b80-7a01-8000-000000000509") });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        bodies.push(JSON.parse(String(init?.body)) as unknown);
        return response.promise;
      }),
    );
    const onReload = vi.fn(async () => undefined);
    renderWriter(onReload);

    const editor = screen.getByRole("textbox", { name: "Narrative paragraph 1" });
    expect(screen.getByRole("region", { name: "Narrative paragraph 1 structure actions" })).toBeTruthy();
    fireEvent.change(editor, { target: { value: "First checkpoint" } });
    fireEvent.blur(editor);
    expect(await screen.findByText("Saving checkpoint…")).toBeTruthy();
    fireEvent.change(editor, { target: { value: "First checkpoint, with a newer ending" } });
    expect(screen.getByText("Saving older checkpoint · newer text remains unsaved")).toBeTruthy();

    response.resolve(jsonResponse(commitReceipt("First checkpoint", "2")));
    expect(await screen.findByText("Unsaved · checkpoints after 750 ms")).toBeTruthy();
    expect((editor as HTMLTextAreaElement).value).toBe("First checkpoint, with a newer ending");
    expect(onReload).toHaveBeenCalledOnce();
    expect(bodies).toHaveLength(1);
    expect(bodies[0]).toMatchObject({
      preconditions: [{ kind: "narrative-node", id: ids.text, revision: "1" }],
      operations: [
        {
          type: "update-authored-text",
          nodeId: ids.text,
          authoredTextPurpose: "spoken",
          language: "und",
          text: "First checkpoint",
        },
      ],
    });
  });

  it("preserves a conflicting draft and exposes explicit retry and reload", async () => {
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => "018f0a60-7b80-7a01-8000-00000000050a") });
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify({ title: "Conflict", status: 409 }), { status: 409 })),
    );
    renderWriter(vi.fn(async () => undefined));

    const editor = screen.getByRole("textbox", { name: "Narrative paragraph 1" });
    fireEvent.change(editor, { target: { value: "Keep this local draft" } });
    fireEvent.blur(editor);

    expect(await screen.findByText("Conflict · local text preserved")).toBeTruthy();
    expect((editor as HTMLTextAreaElement).value).toBe("Keep this local draft");
    expect(screen.getByRole("button", { name: "Refresh revisions for retry" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Reload committed text" })).toBeTruthy();
  });

  it("replays the identical checkpoint body after an ambiguous transport failure", async () => {
    const bodies: string[] = [];
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => "018f0a60-7b80-7a01-8000-00000000050b") });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        bodies.push(String(init?.body));
        if (bodies.length === 1) return new Response("unavailable", { status: 503 });
        return jsonResponse(commitReceipt("Retry exactly", "2"));
      }),
    );
    renderWriter(vi.fn(async () => undefined));

    const editor = screen.getByRole("textbox", { name: "Narrative paragraph 1" });
    fireEvent.change(editor, { target: { value: "Retry exactly" } });
    fireEvent.blur(editor);
    fireEvent.click(await screen.findByRole("button", { name: "Retry identical checkpoint" }));

    await waitFor(() => expect(bodies).toHaveLength(2));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Retry identical checkpoint" })).toBeNull());
    expect(bodies[1]).toBe(bodies[0]);
  });

  it("commits an interior Enter split as one update-plus-insert transaction", async () => {
    const bodies: unknown[] = [];
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => "018f0a60-7b80-7a01-8000-00000000050d") });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        bodies.push(JSON.parse(String(init?.body)) as unknown);
        return jsonResponse(
          structureReceipt(
            [
              { kind: "narrative-node", id: ids.text, before: "1", after: "2" },
              { kind: "narrative-node", id: ids.text2, before: "0", after: "1" },
            ],
            [{ local: "paragraph_018f0a607b807a01800000000000050d", kind: "narrative-node", id: ids.text2 }],
          ),
        );
      }),
    );
    renderWriter(vi.fn(async () => undefined));

    const editor = screen.getByRole("textbox", { name: "Narrative paragraph 1" }) as HTMLTextAreaElement;
    editor.setSelectionRange(9, 9);
    fireEvent.keyDown(editor, { key: "Enter" });

    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toMatchObject({
      preconditions: [
        { kind: "narrative-node", id: ids.text, revision: "1" },
        { kind: "narrative-node", id: ids.root, revision: "1" },
      ],
      operations: [
        { type: "update-authored-text", nodeId: ids.text, text: "Committed" },
        {
          type: "insert-authored-text",
          parentId: ids.root,
          after: { id: ids.text },
          text: " text",
        },
      ],
    });
  });

  it("replays an ambiguous paragraph split with the byte-identical structural body", async () => {
    const bodies: string[] = [];
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => "018f0a60-7b80-7a01-8000-000000000510") });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        bodies.push(String(init?.body));
        if (bodies.length === 1) return new Response("unavailable", { status: 503 });
        return jsonResponse(
          structureReceipt(
            [
              { kind: "narrative-node", id: ids.text, before: "1", after: "2" },
              { kind: "narrative-node", id: ids.text2, before: "0", after: "1" },
            ],
            [{ local: "paragraph_018f0a607b807a018000000000000510", kind: "narrative-node", id: ids.text2 }],
          ),
        );
      }),
    );
    renderWriter(vi.fn(async () => undefined));

    const editor = screen.getByRole("textbox", { name: "Narrative paragraph 1" }) as HTMLTextAreaElement;
    editor.setSelectionRange(9, 9);
    fireEvent.keyDown(editor, { key: "Enter" });
    fireEvent.click(await screen.findByRole("button", { name: "Retry identical checkpoint" }));

    await waitFor(() => expect(bodies).toHaveLength(2));
    expect(bodies[1]).toBe(bodies[0]);
  });

  it("commits Backspace-at-start merge as one update-plus-remove transaction", async () => {
    const bodies: unknown[] = [];
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => "018f0a60-7b80-7a01-8000-00000000050e") });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        bodies.push(JSON.parse(String(init?.body)) as unknown);
        return jsonResponse(
          structureReceipt([
            { kind: "narrative-node", id: ids.text, before: "1", after: "2" },
            { kind: "narrative-node", id: ids.text2, before: "1", after: "2", tombstoned: true },
          ]),
        );
      }),
    );
    renderWriter(
      vi.fn(async () => undefined),
      narrativeWithTwoParagraphs(),
    );

    const editor = screen.getByRole("textbox", { name: "Narrative paragraph 2" }) as HTMLTextAreaElement;
    editor.setSelectionRange(0, 0);
    fireEvent.keyDown(editor, { key: "Backspace" });

    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toMatchObject({
      operations: [
        { type: "update-authored-text", nodeId: ids.text, text: "Committed textSecond paragraph" },
        { type: "remove-narrative-node", nodeId: ids.text2 },
      ],
    });
  });

  it("moves a clean paragraph after its loaded next sibling", async () => {
    const bodies: unknown[] = [];
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => "018f0a60-7b80-7a01-8000-00000000050f") });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        bodies.push(JSON.parse(String(init?.body)) as unknown);
        return jsonResponse(
          structureReceipt([
            { kind: "narrative-node", id: ids.text, before: "1", after: "2" },
            { kind: "narrative-node", id: ids.root, before: "1", after: "2" },
          ]),
        );
      }),
    );
    renderWriter(
      vi.fn(async () => undefined),
      narrativeWithTwoParagraphs(),
    );

    const [moveDown] = screen.getAllByRole("button", { name: "Move paragraph down" });
    if (!moveDown) throw new Error("first paragraph move control is missing");
    fireEvent.click(moveDown);

    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toMatchObject({
      operations: [
        {
          type: "move-narrative-node",
          nodeId: ids.text,
          parentId: ids.root,
          after: { id: ids.text2 },
        },
      ],
    });
  });

  it("creates an explicit Section that inherits its parent language", async () => {
    const bodies: unknown[] = [];
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => "018f0a60-7b80-7a01-8000-000000000512") });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        bodies.push(JSON.parse(String(init?.body)) as unknown);
        return jsonResponse(
          structureReceipt(
            [
              { kind: "narrative-node", id: ids.root, before: "1", after: "2" },
              { kind: "narrative-node", id: ids.section, before: "0", after: "1" },
            ],
            [{ local: "section_018f0a607b807a018000000000000512", kind: "narrative-node", id: ids.section }],
          ),
        );
      }),
    );
    renderWriter(vi.fn(async () => undefined));

    fireEvent.click(screen.getByRole("button", { name: "Add Section" }));
    const title = screen.getByRole("textbox", { name: "New Narrative Section title" });
    fireEvent.change(title, { target: { value: "The human problem" } });
    fireEvent.keyDown(title, { key: "Enter" });

    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toMatchObject({
      preconditions: [{ kind: "narrative-node", id: ids.root, revision: "1" }],
      operations: [
        {
          type: "insert-section",
          parentId: ids.root,
          after: { id: ids.text },
          title: "The human problem",
          language: "und",
        },
      ],
    });
  });

  it("checkpoints a Section title and expands its exact bounded child branch on Enter", async () => {
    const bodies: unknown[] = [];
    const reads: string[] = [];
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => "018f0a60-7b80-7a01-8000-000000000513") });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        if (init?.body !== undefined) {
          bodies.push(JSON.parse(String(init.body)) as unknown);
          return jsonResponse(structureReceipt([{ kind: "narrative-node", id: ids.section, before: "1", after: "2" }]));
        }
        reads.push(String(input));
        return jsonResponse(emptySectionSubtree());
      }),
    );
    renderWriter(
      vi.fn(async () => undefined),
      narrativeWithSection(),
    );

    const title = screen.getByRole("textbox", { name: "Narrative Section Act one" });
    fireEvent.change(title, { target: { value: "Opening" } });
    fireEvent.keyDown(title, { key: "Enter" });

    await waitFor(() => expect(bodies).toHaveLength(1));
    await waitFor(() => expect(reads).toHaveLength(1));
    expect(bodies[0]).toMatchObject({
      preconditions: [{ kind: "narrative-node", id: ids.section, revision: "1" }],
      operations: [
        {
          type: "update-section",
          nodeId: ids.section,
          title: "Opening",
          language: "und",
        },
      ],
    });
    expect(reads[0]).toContain(`parentId=${ids.section}`);
    expect(screen.getAllByRole("textbox", { name: "New Narrative paragraph" })).toContain(document.activeElement);
    expect((screen.getByRole("button", { name: "Remove empty Section" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("keeps Section storage failures private and retries the child branch", async () => {
    let attempts = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        attempts += 1;
        if (attempts === 1) {
          throw new Error("sqlite read failed at /Users/editor/Library/Application Support/Open Cut/project.db");
        }
        return jsonResponse(emptySectionSubtree());
      }),
    );
    renderWriter(
      vi.fn(async () => undefined),
      narrativeWithSection(),
    );

    fireEvent.click(screen.getByRole("button", { name: "Expand Section" }));
    expect(await screen.findByText("Could not load this Section.")).toBeTruthy();
    expect(screen.queryByText(/sqlite|Application Support|project\.db/i)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry Section read" }));

    await waitFor(() => expect(screen.queryByText("Could not load this Section.")).toBeNull());
    expect(attempts).toBe(2);
  });

  it("reopens the exact selected Section path after the Story surface remounts", async () => {
    const reads: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request) => {
        reads.push(String(input));
        return jsonResponse(emptySectionSubtree());
      }),
    );

    renderWriter(
      vi.fn(async () => undefined),
      narrativeWithSection(),
      [durableID(ids.section)],
    );

    await waitFor(() => expect(reads).toHaveLength(1));
    expect(screen.getByRole("button", { name: "Collapse Section" })).toBeTruthy();
    expect(screen.getAllByRole("textbox", { name: "New Narrative paragraph" })).toHaveLength(2);
  });
});

function renderWriter(
  onReload: () => Promise<void>,
  value: NarrativeSubtree = narrative(),
  activeSectionPath?: readonly ReturnType<typeof durableID>[],
) {
  const base = createContracts();
  const contracts = { ...base, start: () => undefined, close: () => undefined };
  return render(
    <ContractsProvider contracts={contracts}>
      <CreatorNarrativeWriter
        activeSectionPath={activeSectionPath}
        narrative={value}
        onReload={onReload}
        onSelect={() => undefined}
        projectId={durableID(ids.project)}
        projectRevision={revisionString("1")}
        sequenceId={durableID(ids.sequence)}
      />
    </ContractsProvider>,
  );
}

function narrativeWithTwoParagraphs(): NarrativeSubtree {
  const value = narrative();
  return {
    ...value,
    nodes: [
      ...value.nodes,
      {
        kind: "authored-text",
        authoredText: {
          id: durableID(ids.text2),
          revision: revisionString("1"),
          documentId: durableID(ids.document),
          parentId: durableID(ids.root),
          afterNodeId: durableID(ids.text),
          purpose: "spoken",
          language: "und",
          text: "Second paragraph",
          tombstoned: false,
        },
      },
    ],
  };
}

function narrativeWithSection(): NarrativeSubtree {
  const value = narrative();
  return {
    ...value,
    nodes: [
      {
        kind: "section",
        section: {
          id: durableID(ids.section),
          revision: revisionString("1"),
          documentId: durableID(ids.document),
          parentId: durableID(ids.root),
          title: "Act one",
          language: "und",
          tombstoned: false,
        },
      },
      ...value.nodes,
    ],
  };
}

function emptySectionSubtree(): NarrativeSubtree {
  return {
    documentId: durableID(ids.document),
    documentRevision: revisionString("2"),
    parent: {
      id: durableID(ids.section),
      revision: revisionString("2"),
      title: "Opening",
      language: "und",
    },
    nodes: [],
    activityCursor: "2" as NarrativeSubtree["activityCursor"],
  };
}

function narrative(): NarrativeSubtree {
  return {
    documentId: durableID(ids.document),
    documentRevision: revisionString("1"),
    parent: {
      id: durableID(ids.root),
      revision: revisionString("1"),
      title: "Story",
      language: "und",
    },
    nodes: [
      {
        kind: "authored-text",
        authoredText: {
          id: durableID(ids.text),
          revision: revisionString("1"),
          documentId: durableID(ids.document),
          parentId: durableID(ids.root),
          purpose: "spoken",
          language: "und",
          text: "Committed text",
          tombstoned: false,
        },
      },
    ],
    activityCursor: "1" as NarrativeSubtree["activityCursor"],
  };
}

function commitReceipt(text: string, revision: string) {
  return {
    proposal: {
      id: ids.proposal,
      projectId: ids.project,
      sequenceId: ids.sequence,
      requestId: "ui:creator-edit",
      actor: { kind: "creator", creatorId: ids.creator },
      status: "applied",
      appliedTransactionId: ids.transaction,
      allocation: [],
    },
    transaction: {
      id: ids.transaction,
      proposalId: ids.proposal,
      projectId: ids.project,
      actor: { kind: "creator", creatorId: ids.creator },
      committedProjectRevision: "2",
      changes: [{ kind: "narrative-node", id: ids.text, before: "1", after: revision }],
      operations: [{ text }],
    },
    activityCursor: "2",
    replayed: false,
  };
}

function structureReceipt(changes: unknown[], allocation: unknown[] = []) {
  return {
    proposal: {
      id: ids.proposal,
      projectId: ids.project,
      sequenceId: ids.sequence,
      requestId: "ui:creator-edit:structure",
      actor: { kind: "creator", creatorId: ids.creator },
      status: "applied",
      appliedTransactionId: ids.transaction,
      allocation,
    },
    transaction: {
      id: ids.transaction,
      proposalId: ids.proposal,
      projectId: ids.project,
      actor: { kind: "creator", creatorId: ids.creator },
      committedProjectRevision: "2",
      changes,
    },
    activityCursor: "2",
    replayed: false,
  };
}

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}

function deferred<Value>() {
  let resolve!: (value: Value) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<Value>((accept, decline) => {
    resolve = accept;
    reject = decline;
  });
  return { promise, resolve, reject };
}
