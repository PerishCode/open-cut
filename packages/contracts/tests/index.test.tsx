// @vitest-environment jsdom

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { StrictMode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ContractsProvider, createContracts, runtimePeer, useProjects, usePutProject } from "../src/index.js";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("product runtime peer contracts", () => {
  it("keeps Web discovery identifiers in one pure public contract", () => {
    expect(runtimePeer.web).toEqual({ app: "web", httpEndpoint: "http" });
  });

  it("adapts generated OpenAPI reads and writes behind stable ports", async () => {
    const fetchMock = vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/v1/projects" && init?.method !== "PUT") {
        return jsonResponse({ revision: 3, projects: [{ id: "alpha", name: "Alpha", description: "First" }] });
      }
      if (url === "/api/v1/projects/beta" && init?.method === "PUT") {
        expect(init.body).toBe(JSON.stringify({ name: "Beta", description: "Second" }));
        return jsonResponse({
          revision: 4,
          project: { id: "beta", name: "Beta", description: "Second" },
        });
      }
      throw new Error(`unexpected request ${init?.method ?? "GET"} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    const contracts = createContracts();

    await expect(contracts.projects.read.list()).resolves.toMatchObject({ revision: 3 });
    await expect(
      contracts.projects.write.put({ id: "beta", name: "Beta", description: "Second" }),
    ).resolves.toMatchObject({ revision: 4 });
    expect(contracts.projects.read.getSnapshot()).toMatchObject({
      status: "ready",
      revision: 4,
      projects: [{ id: "alpha" }, { id: "beta" }],
    });
  });

  it("exposes snapshot and write ports as React hooks while SSE reconciles state", async () => {
    vi.stubGlobal("fetch", vi.fn(contractFetch));
    const view = render(
      <StrictMode>
        <ContractsProvider>
          <ProjectConsumer />
        </ContractsProvider>
      </StrictMode>,
    );

    expect(await screen.findByText("ready:1:alpha")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Add Beta" }));
    expect(await screen.findByText("ready:2:alpha,beta")).toBeTruthy();
    view.unmount();

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/events",
        expect.objectContaining({ headers: { accept: "text/event-stream" }, signal: expect.any(AbortSignal) }),
      );
    });
  });
});

function ProjectConsumer() {
  const state = useProjects();
  const write = usePutProject();
  return (
    <>
      <output>{[state.status, state.revision, state.projects.map((project) => project.id).join(",")].join(":")}</output>
      <button type="button" onClick={() => void write.put({ id: "beta", name: "Beta", description: "Second" })}>
        Add Beta
      </button>
    </>
  );
}

async function contractFetch(input: string | URL | Request, init?: RequestInit): Promise<Response> {
  const url = String(input);
  if (url === "/api/v1/projects") {
    return jsonResponse({ revision: 1, projects: [{ id: "alpha", name: "Alpha", description: "First" }] });
  }
  if (url === "/api/v1/projects/beta" && init?.method === "PUT") {
    return jsonResponse({ revision: 2, project: { id: "beta", name: "Beta", description: "Second" } });
  }
  if (url === "/api/v1/events") return eventStream(init?.signal);
  throw new Error(["unexpected request", init?.method ?? "GET", url].join(" "));
}

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}

function eventStream(signal?: AbortSignal | null): Response {
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(
        new TextEncoder().encode(
          'event: project.snapshot\ndata: {"revision":1,"projects":[{"id":"alpha","name":"Alpha","description":"First"}]}\n\n',
        ),
      );
      signal?.addEventListener("abort", () => controller.close(), { once: true });
    },
  });
  return new Response(body, { status: 200, headers: { "content-type": "text/event-stream" } });
}
