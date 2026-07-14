import { getWatchProjectsUrl, listProjects, putProject } from "@open-cut/openapi";

import { EventBus } from "./event-bus.js";
import { readServerEvents } from "./sse.js";

export type Project = Readonly<{
  id: string;
  name: string;
  description: string;
}>;

export type ProjectSnapshot = Readonly<{
  revision: number;
  projects: readonly Project[];
}>;

export type ProjectUpserted = Readonly<{
  revision: number;
  project: Project;
}>;

export type ProjectState = Readonly<{
  status: "connecting" | "ready" | "unavailable";
  revision: number;
  projects: readonly Project[];
  error?: Error;
}>;

export interface ProjectReadPort {
  list(signal?: AbortSignal): Promise<ProjectSnapshot>;
  getSnapshot(): ProjectState;
  subscribe(listener: () => void): () => void;
}

export interface ProjectWritePort {
  put(project: Project, signal?: AbortSignal): Promise<ProjectUpserted>;
}

type ProjectEvents = {
  "project.snapshot": ProjectSnapshot;
  "project.upserted": ProjectUpserted;
};

export type ProjectPorts = Readonly<{
  read: ProjectReadPort;
  write: ProjectWritePort;
}>;

export type Contracts = Readonly<{
  projects: ProjectPorts;
  start(): void;
  close(): void;
}>;

const initialState: ProjectState = { status: "connecting", revision: 0, projects: [] };

export function createContracts(): Contracts {
  const events = new EventBus<ProjectEvents>();
  const listeners = new Set<() => void>();
  let state = initialState;
  let stream: AbortController | undefined;
  let started = false;

  const update = (next: ProjectState): void => {
    state = next;
    for (const listener of listeners) listener();
  };
  const applySnapshot = (snapshot: ProjectSnapshot): void => {
    if (snapshot.revision < state.revision) return;
    update({ status: "ready", revision: snapshot.revision, projects: snapshot.projects });
  };
  const applyUpsert = (event: ProjectUpserted): void => {
    if (event.revision <= state.revision) return;
    if (event.revision !== state.revision + 1) {
      void read.list(stream?.signal).catch(() => undefined);
      return;
    }
    const projects = state.projects.filter((project) => project.id !== event.project.id).concat(event.project);
    projects.sort((left, right) => left.id.localeCompare(right.id));
    update({ status: "ready", revision: event.revision, projects });
  };
  events.subscribe("project.snapshot", applySnapshot);
  events.subscribe("project.upserted", applyUpsert);

  const read: ProjectReadPort = {
    list: async (signal) => {
      const response = await listProjects({ signal });
      if (response.status !== 200) throw new Error(`list projects returned ${response.status}`);
      const snapshot = normalizeSnapshot(response.data);
      events.publish("project.snapshot", snapshot);
      return snapshot;
    },
    getSnapshot: () => state,
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
  const write: ProjectWritePort = {
    put: async (project, signal) => {
      const response = await putProject(
        project.id,
        { name: project.name, description: project.description },
        { signal },
      );
      if (response.status !== 200) throw new Error(`put project returned ${response.status}`);
      const event = normalizeUpsert(response.data);
      events.publish("project.upserted", event);
      return event;
    },
  };

  const runStream = async (signal: AbortSignal): Promise<void> => {
    let retry = 1000;
    while (!signal.aborted) {
      try {
        const response = await fetch(getWatchProjectsUrl(), {
          headers: { accept: "text/event-stream" },
          signal,
        });
        for await (const event of readServerEvents(response)) {
          if (event.retry !== undefined) retry = event.retry;
          if (event.event === "project.snapshot") events.publish("project.snapshot", normalizeSnapshot(event.data));
          if (event.event === "project.upserted") events.publish("project.upserted", normalizeUpsert(event.data));
        }
        if (!signal.aborted) throw new Error("event stream ended");
      } catch (error) {
        if (signal.aborted) return;
        update({ ...state, status: "unavailable", error: asError(error) });
      }
      await abortableDelay(retry, signal);
    }
  };

  return {
    projects: { read, write },
    start: () => {
      if (started) return;
      started = true;
      const controller = new AbortController();
      stream = controller;
      update({ ...state, status: "connecting", error: undefined });
      void read.list(controller.signal).catch((error: unknown) => {
        if (!controller.signal.aborted && stream === controller) {
          update({ ...state, status: "unavailable", error: asError(error) });
        }
      });
      void runStream(controller.signal);
    },
    close: () => {
      started = false;
      stream?.abort();
      stream = undefined;
    },
  };
}

function normalizeSnapshot(value: unknown): ProjectSnapshot {
  const candidate = asRecord(value);
  if (!Number.isSafeInteger(candidate.revision) || typeof candidate.revision !== "number" || candidate.revision < 0) {
    throw new Error("project snapshot has an invalid revision");
  }
  if (candidate.projects !== null && !Array.isArray(candidate.projects)) {
    throw new Error("project snapshot has invalid projects");
  }
  const projects = (candidate.projects ?? []).map(normalizeProject);
  projects.sort((left, right) => left.id.localeCompare(right.id));
  return { revision: candidate.revision, projects };
}

function normalizeUpsert(value: unknown): ProjectUpserted {
  const candidate = asRecord(value);
  if (!Number.isSafeInteger(candidate.revision) || typeof candidate.revision !== "number" || candidate.revision < 1) {
    throw new Error("project event has an invalid revision");
  }
  return { revision: candidate.revision, project: normalizeProject(candidate.project) };
}

function normalizeProject(value: unknown): Project {
  const project = asRecord(value);
  if (typeof project.id !== "string" || typeof project.name !== "string" || typeof project.description !== "string") {
    throw new Error("project payload is invalid");
  }
  return { id: project.id, name: project.name, description: project.description };
}

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("project payload is invalid");
  return value as Record<string, unknown>;
}

function asError(value: unknown): Error {
  return value instanceof Error ? value : new Error(String(value));
}

function abortableDelay(milliseconds: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const timeout = setTimeout(resolve, milliseconds);
    signal.addEventListener(
      "abort",
      () => {
        clearTimeout(timeout);
        resolve();
      },
      { once: true },
    );
  });
}
