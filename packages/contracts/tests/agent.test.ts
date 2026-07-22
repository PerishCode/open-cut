import { afterEach, describe, expect, it, vi } from "vitest";
import type { AgentRun } from "../src/agent.js";
import { createAgentBridgePort } from "../src/agent.js";
import { cursorString, durableID, revisionString } from "../src/exact.js";

const projectId = durableID("018f0a60-7b80-7a01-8000-000000000301");
const runId = durableID("018f0a60-7b80-7a01-8000-000000000302");
const turnId = durableID("018f0a60-7b80-7a01-8000-000000000303");
const messageId = durableID("018f0a60-7b80-7a01-8000-000000000304");

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Creator Agent bridge contract", () => {
  it("normalizes only safe closed local availability", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse({
          adapterId: "codex-cli-v1",
          promptVersion: "open-cut-agent-v2",
          state: "available",
          version: "codex-cli 0.144.4",
          executable: "/must/not/escape",
        }),
      ),
    );
    const availability = await createAgentBridgePort().availability();
    expect(availability).toEqual({
      adapterId: "codex-cli-v1",
      promptVersion: "open-cut-agent-v2",
      state: "available",
      version: "codex-cli 0.144.4",
    });
    expect(JSON.stringify(availability)).not.toContain("executable");
  });

  it("normalizes the safe durable ledger and never forwards adapter internals", async () => {
    const requests: Array<{ url: string; body?: unknown }> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        requests.push({
          url: String(input),
          ...(init?.body ? { body: JSON.parse(String(init.body)) as unknown } : {}),
        });
        if (String(input).endsWith("/conversation?limit=50")) {
          return jsonResponse({
            projectId,
            runId,
            messages: [conversationMessage("creator", "Make a concise opening", "1")],
          });
        }
        if (String(input).endsWith("/agent/runs?limit=10")) {
          return jsonResponse({ projectId, runs: [agentRun()] });
        }
        return jsonResponse({
          run: agentRun(),
          message: conversationMessage("creator", "Make a concise opening", "1"),
          replayed: false,
          nativeSessionId: "must-be-dropped",
          adapter: "must-be-dropped",
        });
      }),
    );
    const port = createAgentBridgePort();
    const attachment = { kind: "asset" as const, entity: { id: messageId, revision: revisionString("1") } };
    const submitted = await port.begin(projectId, {
      requestId: "creator:agent:begin:1",
      message: "Make a concise opening",
      attachments: [attachment],
    });
    expect(submitted.run.currentTurn.generation).toBe(revisionString("1"));
    expect(JSON.stringify(submitted)).not.toContain("nativeSession");
    expect(JSON.stringify(submitted)).not.toContain("adapter");
    expect(requests[0]).toEqual({
      url: `/api/v1/projects/${projectId}/agent/runs`,
      body: { requestId: "creator:agent:begin:1", message: "Make a concise opening", attachments: [attachment] },
    });
    const page = await port.conversation(projectId, runId, { limit: 50 });
    expect(page.messages).toHaveLength(1);
    expect(page.messages[0]?.role).toBe("creator");
    expect((await port.list(projectId)).runs[0]?.id).toBe(runId);
  });

  it("accepts only the closed durable conversation notice", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse({
          projectId,
          runId,
          messages: [conversationMessage("notice", "context-rebuilt", "1")],
        }),
      ),
    );
    const page = await createAgentBridgePort().conversation(projectId, runId);
    expect(page.messages[0]).toMatchObject({ role: "notice", text: "context-rebuilt" });
  });

  it("reconciles the Turn receipt ledger independently from conversation", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse({
          projectId,
          runId,
          turnId,
          receipts: [
            {
              schema: "open-cut/command-receipt/v2",
              id: messageId,
              projectId,
              runId,
              turnId,
              ordinal: "1",
              class: "outcome",
              command: "edit apply",
              commandFingerprint: `sha256:${"a".repeat(64)}`,
              inputDigest: `sha256:${"b".repeat(64)}`,
              requestId: "agent:edit:apply:1",
              resultDigest: `sha256:${"c".repeat(64)}`,
              status: "succeeded",
              resultRefs: [{ kind: "edit-transaction", id: messageId, revision: "2" }],
              projectRevision: "2",
              activityCursor: "9",
              createdAt: "2026-07-16T08:00:02Z",
            },
          ],
        }),
      ),
    );
    const page = await createAgentBridgePort().receipts(projectId, runId, turnId, { limit: 50 });
    expect(page.receipts[0]).toMatchObject({ class: "outcome", command: "edit apply", ordinal: "1" });
    expect(page.receipts[0]?.resultRefs[0]).toMatchObject({ kind: "edit-transaction", revision: "2" });
  });

  it("reads authoritative historical Turns without deriving them from conversation", async () => {
    const requested: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request) => {
        requested.push(String(input));
        return jsonResponse({
          projectId,
          runId,
          turns: [
            {
              id: messageId,
              generation: "2",
              status: "active",
              startedAt: "2026-07-16T08:00:02Z",
            },
            { ...agentRun().currentTurn, status: "completed", endedAt: "2026-07-16T08:00:01Z" },
          ],
        });
      }),
    );
    const page = await createAgentBridgePort().turns(projectId, runId, { limit: 50 });
    expect(page.turns.map((turn) => turn.generation)).toEqual(["2", "1"]);
    expect(requested).toEqual([`/api/v1/projects/${projectId}/agent/runs/${runId}/turns?limit=50`]);
  });

  it("binds Stop to the exact current Turn and generation", async () => {
    const requests: Array<{ url: string; body?: unknown }> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        requests.push({
          url: String(input),
          ...(init?.body ? { body: JSON.parse(String(init.body)) as unknown } : {}),
        });
        return jsonResponse({
          run: { ...agentRun(), status: "paused", waitingReason: "creator-interrupted" },
          replayed: false,
        });
      }),
    );
    const port = createAgentBridgePort();
    const interrupted = await port.interrupt(projectId, normalizeRunForInput(), "creator:agent:interrupt:1");
    expect(interrupted.status).toBe("paused");
    expect(requests[0]).toEqual({
      url: `/api/v1/projects/${projectId}/agent/runs/${runId}/turns/${turnId}/interrupt`,
      body: { requestId: "creator:agent:interrupt:1", expectedGeneration: "1" },
    });
  });

  it("decodes only the bounded process-local presentation union", async () => {
    const frame = `event: presentation\ndata: ${JSON.stringify({
      runId,
      turnId,
      sequence: "1",
      kind: "tool-started",
      tool: "command",
      native: "must-be-dropped",
    })}\n\n`;
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        const body = new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(new TextEncoder().encode(frame));
            controller.close();
          },
        });
        return new Response(body, { status: 200, headers: { "content-type": "text/event-stream" } });
      }),
    );
    const events: unknown[] = [];
    await createAgentBridgePort().watchPresentation(
      projectId,
      runId,
      turnId,
      (event) => events.push(event),
      new AbortController().signal,
    );
    expect(events).toEqual([{ runId, turnId, sequence: "1", kind: "tool-started", tool: "command" }]);
    expect(JSON.stringify(events)).not.toContain("native");
  });
});

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}

function agentRun() {
  return {
    id: runId,
    projectId,
    intent: "Make a concise opening",
    status: "active",
    currentTurn: {
      id: turnId,
      generation: "1",
      status: "active",
      startedAt: "2026-07-16T08:00:00Z",
      nativeSessionId: "must-be-dropped",
    },
    activityCursor: "9",
    createdAt: "2026-07-16T08:00:00Z",
    updatedAt: "2026-07-16T08:00:01Z",
  };
}

function conversationMessage(role: "creator" | "agent" | "notice", text: string, ordinal: string) {
  return {
    id: messageId,
    projectId,
    runId,
    turnId,
    ordinal,
    role,
    text,
    attachments: [],
    createdAt: "2026-07-16T08:00:00Z",
  };
}

function normalizeRunForInput(): AgentRun {
  return {
    id: runId,
    projectId,
    intent: "Make a concise opening",
    status: "active" as const,
    currentTurn: {
      id: turnId,
      generation: revisionString("1"),
      status: "active" as const,
      startedAt: "2026-07-16T08:00:00Z",
    },
    activityCursor: cursorString("9"),
    createdAt: "2026-07-16T08:00:00Z",
    updatedAt: "2026-07-16T08:00:01Z",
  };
}
