// @vitest-environment jsdom

import { ContractsProvider, durableID, revisionString } from "@open-cut/contracts";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { CreatorAgentPane } from "../../src/components/creator-agent-pane.js";

const originalScrollIntoView = Object.getOwnPropertyDescriptor(Element.prototype, "scrollIntoView");

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  if (originalScrollIntoView) {
    Object.defineProperty(Element.prototype, "scrollIntoView", originalScrollIntoView);
  } else {
    Reflect.deleteProperty(Element.prototype, "scrollIntoView");
  }
});

describe("CreatorAgentPane", () => {
  it("creates only from New task and explicitly continues a paused Run", async () => {
    const projectId = durableID("018f0a60-7b80-7a01-8000-000000000401");
    const sequenceId = durableID("018f0a60-7b80-7a01-8000-000000000402");
    const runId = "018f0a60-7b80-7a01-8000-000000000403";
    const firstTurnId = "018f0a60-7b80-7a01-8000-000000000404";
    const secondTurnId = "018f0a60-7b80-7a01-8000-000000000405";
    const requestUUIDs = ["018f0a60-7b80-7a01-8000-000000000406", "018f0a60-7b80-7a01-8000-000000000407"];
    const attachment = { kind: "asset" as const, entity: { id: sequenceId, revision: revisionString("1") } };
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => requestUUIDs.shift()) });
    const submissions: unknown[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url === "/api/v1/projects") return jsonResponse({ projects: [], activityCursor: "0" });
        if (url === "/api/v1/events?after=0") return eventStream(init?.signal);
        if (url === "/api/v1/agent/availability") {
          return jsonResponse({
            adapterId: "codex-cli-v1",
            promptVersion: "open-cut-agent-v2",
            state: "available",
            version: "codex-cli 0.144.4",
          });
        }
        if (url === `/api/v1/projects/${projectId}/agent/runs?limit=10`) {
          return jsonResponse({ projectId, runs: [] });
        }
        if (url === `/api/v1/projects/${projectId}/agent/runs`) {
          submissions.push(JSON.parse(String(init?.body)) as unknown);
          return jsonResponse(submission(projectId, runId, firstTurnId, "1", "Draft a sharp opening", "1"));
        }
        if (url === `/api/v1/projects/${projectId}/agent/runs/${runId}/messages`) {
          submissions.push(JSON.parse(String(init?.body)) as unknown);
          return jsonResponse(submission(projectId, runId, secondTurnId, "2", "Make it warmer", "2"));
        }
        throw new Error(`unexpected request ${init?.method ?? "GET"} ${url}`);
      }),
    );

    const view = render(
      <ContractsProvider>
        <CreatorAgentPane
          contextCandidates={[{ key: "asset:current", label: "Current asset", attachment }]}
          projectId={projectId}
          sequenceId={sequenceId}
        />
      </ContractsProvider>,
    );
    expect(await screen.findByText("Ready · codex-cli 0.144.4")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Add @ Current asset" }));
    fireEvent.change(screen.getByRole("textbox", { name: "New task · Ctrl/⌘ Enter" }), {
      target: { value: "Draft a sharp opening" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Start task" }));
    expect(await screen.findByText("Draft a sharp opening")).toBeTruthy();
    expect(submissions[0]).toEqual({
      requestId: "ui:agent-begin:018f0a60-7b80-7a01-8000-000000000406",
      message: "Draft a sharp opening",
      sequenceId,
      attachments: [attachment],
    });

    const composer = screen.getByRole("textbox", { name: "Continue this task · Ctrl/⌘ Enter" });
    expect(composer.getAttribute("aria-keyshortcuts")).toBe("Control+Enter Meta+Enter");
    fireEvent.change(composer, {
      target: { value: "Make it warmer" },
    });
    fireEvent.keyDown(composer, { key: "Enter" });
    expect(submissions).toHaveLength(1);
    fireEvent.keyDown(composer, { ctrlKey: true, key: "Enter" });
    await waitFor(() => expect(submissions).toHaveLength(2));
    expect(submissions[1]).toEqual({
      requestId: "ui:agent-continue:018f0a60-7b80-7a01-8000-000000000407",
      expectedGeneration: "1",
      message: "Make it warmer",
      sequenceId,
      attachments: [],
    });
    expect(await screen.findByText("Make it warmer")).toBeTruthy();
    view.unmount();
  });

  it("loads authoritative historical Turns and focuses receipt refs without merging ledgers", async () => {
    const projectId = durableID("018f0a60-7b80-7a01-8000-000000000421");
    const sequenceId = durableID("018f0a60-7b80-7a01-8000-000000000422");
    const runId = durableID("018f0a60-7b80-7a01-8000-000000000423");
    const firstTurnId = durableID("018f0a60-7b80-7a01-8000-000000000424");
    const secondTurnId = durableID("018f0a60-7b80-7a01-8000-000000000425");
    const receiptId = durableID("018f0a60-7b80-7a01-8000-000000000426");
    const transactionId = durableID("018f0a60-7b80-7a01-8000-000000000427");
    const creatorMessageId = durableID("018f0a60-7b80-7a01-8000-000000000428");
    const agentMessageId = durableID("018f0a60-7b80-7a01-8000-000000000429");
    const run = submission(projectId, runId, secondTurnId, "2", "Continue", "2").run;
    const scrollIntoView = vi.fn();
    Object.defineProperty(Element.prototype, "scrollIntoView", {
      configurable: true,
      value: scrollIntoView,
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request) => {
        const url = String(input);
        if (url === "/api/v1/projects") return jsonResponse({ projects: [], activityCursor: "0" });
        if (url === "/api/v1/events?after=0") return eventStream();
        if (url === "/api/v1/agent/availability") {
          return jsonResponse({
            adapterId: "codex-cli-v1",
            promptVersion: "open-cut-agent-v2",
            state: "available",
            version: "codex-cli 0.144.4",
          });
        }
        if (url === `/api/v1/projects/${projectId}/agent/runs?limit=10`) {
          return jsonResponse({ projectId, runs: [run] });
        }
        if (url === `/api/v1/projects/${projectId}/agent/runs/${runId}`) return jsonResponse(run);
        if (url === `/api/v1/projects/${projectId}/agent/runs/${runId}/conversation?limit=100`) {
          return jsonResponse({
            projectId,
            runId,
            messages: [
              {
                id: creatorMessageId,
                projectId,
                runId,
                turnId: secondTurnId,
                ordinal: "1",
                role: "creator",
                text: "Make the `ending` concise.",
                attachments: [],
                createdAt: "2026-07-16T07:00:00Z",
              },
              {
                id: agentMessageId,
                projectId,
                runId,
                turnId: secondTurnId,
                ordinal: "2",
                role: "agent",
                text: "The ending is now concise.\n\nApplied with `edit apply`.",
                attachments: [],
                createdAt: "2026-07-16T07:00:01Z",
              },
            ],
          });
        }
        if (url === `/api/v1/projects/${projectId}/agent/runs/${runId}/turns?limit=100`) {
          return jsonResponse({
            projectId,
            runId,
            turns: [
              run.currentTurn,
              { id: firstTurnId, generation: "1", status: "completed", startedAt: "2026-07-16T07:00:00Z" },
            ],
          });
        }
        if (url === `/api/v1/projects/${projectId}/agent/runs/${runId}/turns/${secondTurnId}/receipts?limit=100`) {
          return jsonResponse({ projectId, runId, turnId: secondTurnId, receipts: [] });
        }
        if (url === `/api/v1/projects/${projectId}/agent/runs/${runId}/turns/${firstTurnId}/receipts?limit=100`) {
          return jsonResponse({
            projectId,
            runId,
            turnId: firstTurnId,
            receipts: [
              {
                schema: "open-cut/command-receipt/v2",
                id: receiptId,
                projectId,
                runId,
                turnId: firstTurnId,
                ordinal: "1",
                class: "outcome",
                command: "edit apply",
                commandFingerprint: `sha256:${"a".repeat(64)}`,
                inputDigest: `sha256:${"b".repeat(64)}`,
                resultDigest: `sha256:${"c".repeat(64)}`,
                status: "succeeded",
                resultRefs: [
                  { kind: "transaction", id: transactionId },
                  { kind: "caption", id: receiptId, revision: "1" },
                ],
                projectRevision: "9",
                activityCursor: "12",
                createdAt: "2026-07-16T07:00:01Z",
              },
            ],
          });
        }
        throw new Error(`unexpected request ${url}`);
      }),
    );
    const focus = vi.fn(() => "Caption receipt r1; current workspace is r2.");
    render(
      <ContractsProvider>
        <CreatorAgentPane
          contextCandidates={[]}
          onFocusReceiptRef={focus}
          projectId={projectId}
          sequenceId={sequenceId}
        />
      </ContractsProvider>,
    );
    expect(await screen.findByText("COMMAND RECEIPTS · TURN 2")).toBeTruthy();
    expect(screen.getByText("Make the `ending` concise.").tagName).toBe("P");
    expect(screen.getByText("edit apply").tagName).toBe("CODE");
    await waitFor(() =>
      expect(scrollIntoView).toHaveBeenCalledWith({
        block: "start",
        inline: "nearest",
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Turn 1 · completed" }));
    const outcome = await screen.findByText("Creative change committed");
    const latestResponse = screen.getByText("Agent response").closest("article");
    expect(latestResponse).toBeTruthy();
    expect(latestResponse?.compareDocumentPosition(outcome.closest("article") as Node)).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );
    expect(screen.getByText("edit apply · Project r9")).toBeTruthy();
    expect(screen.queryByText("CONVERSATION · 0 MESSAGES")).toBeNull();
    expect(screen.queryByText("OUTCOME · #1")).toBeNull();
    expect(screen.queryByText(transactionId)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Show 1 receipt" }));
    expect(screen.getByText("OUTCOME · #1")).toBeTruthy();
    expect(screen.getByText("Activity #12")).toBeTruthy();
    expect(screen.queryByText(transactionId)).toBeNull();
    expect(screen.getByText("Transaction")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Focus Caption" }));
    expect(focus).toHaveBeenCalledWith({ kind: "caption", id: receiptId, revision: "1" });
    expect(await screen.findByText("Caption receipt r1; current workspace is r2.")).toBeTruthy();
    expect(scrollIntoView).toHaveBeenCalledTimes(1);
  });
});

function submission(
  projectId: string,
  runId: string,
  turnId: string,
  generation: string,
  message: string,
  ordinal: string,
) {
  return {
    run: {
      id: runId,
      projectId,
      intent: "Draft a sharp opening",
      status: "paused",
      waitingReason: "awaiting-creator",
      currentTurn: {
        id: turnId,
        generation,
        status: "completed",
        startedAt: "2026-07-16T08:00:00Z",
        endedAt: "2026-07-16T08:00:01Z",
      },
      activityCursor: ordinal,
      createdAt: "2026-07-16T08:00:00Z",
      updatedAt: "2026-07-16T08:00:01Z",
    },
    message: {
      id: `018f0a60-7b80-7a01-8000-00000000041${ordinal}`,
      projectId,
      runId,
      turnId,
      ordinal,
      role: "creator",
      text: message,
      attachments: [],
      createdAt: "2026-07-16T08:00:00Z",
    },
    replayed: false,
  };
}

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}

function eventStream(signal?: AbortSignal | null): Response {
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      signal?.addEventListener("abort", () => controller.close(), { once: true });
    },
  });
  return new Response(body, { status: 200, headers: { "content-type": "text/event-stream" } });
}
