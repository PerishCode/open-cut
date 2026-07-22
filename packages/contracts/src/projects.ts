import { getWatchActivityUrl } from "@open-cut/openapi/activity";
import { createProject, listProjects, showProject } from "@open-cut/openapi/projects";
import { type AgentBridgePort, createAgentBridgePort } from "./agent.js";
import { type AuthorizationPort, createAuthorizationPort } from "./authorization.js";
import { createEditingPorts, type EditingPorts } from "./editing.js";
import { EventBus } from "./event-bus.js";
import {
  type CursorString,
  cursorString,
  type DigestString,
  type DurableID,
  digestString,
  durableID,
  type Int64String,
  incrementCursor,
  int64String,
  type RevisionString,
  revisionString,
} from "./exact.js";
import { createSequenceExportPort, type SequenceExportPort } from "./exports.js";
import { createMediaPorts, type MediaPorts } from "./media.js";
import { createProductStatusPort, type ProductStatusPort } from "./product.js";
import { createProjectVersionPort, type ProjectVersionPort } from "./project-versions.js";
import { createProductResourcePort, type ProductResourcePort } from "./resources.js";
import { readServerEvents } from "./sse.js";

export type RationalTime = Readonly<{ value: Int64String; scale: number }>;

export type SequenceFormat = Readonly<{
  canvasWidth: number;
  canvasHeight: number;
  pixelAspect: RationalTime;
  frameRate: RationalTime;
  audioSampleRate: number;
  audioLayout: "stereo";
  colorPolicy: "sdr-rec709";
}>;

export type Project = Readonly<{
  id: DurableID;
  revision: RevisionString;
  lifecycleRevision: RevisionString;
  name: string;
  status: "active" | "archived" | "tombstoned";
  narrativeDocumentId: DurableID;
  mainSequenceId: DurableID;
}>;

export type Track = Readonly<{
  id: DurableID;
  revision: RevisionString;
  type: "video" | "audio" | "caption";
  label: string;
}>;

export type ProjectOverview = Readonly<{
  project: Project;
  narrativeDocumentRevision: RevisionString;
  narrativeRootNodeId: DurableID;
  mainSequenceRevision: RevisionString;
  format: SequenceFormat;
  tracks: readonly Track[];
  activityCursor: CursorString;
}>;

export type ProjectSnapshot = Readonly<{
  projects: readonly Project[];
  activityCursor: CursorString;
  nextAfter?: string;
}>;

export type CreateProjectInput = Readonly<{
  requestId: string;
  name: string;
  format?: SequenceFormat;
}>;

export type ProjectCreated = Readonly<{
  project: ProjectOverview;
  proposalId: DurableID;
  transactionId: DurableID;
  requestDigest: DigestString;
  projectActivityCursor: CursorString;
  installationActivityCursor: CursorString;
  replayed: boolean;
}>;

export type ActivityScope = Readonly<{
  kind: "project" | "installation";
  id: string;
}>;

export type ActivityActor = Readonly<{
  kind: "creator" | "agent";
  id: DurableID;
}>;

export type ChangedEntityRef = Readonly<{
  kind: string;
  id: DurableID;
  revision: RevisionString;
}>;

export type ActivityOutcomeRef = Readonly<{
  kind: string;
  id: string;
}>;

export type ActivityEvent = Readonly<{
  schema: "open-cut/activity/v1";
  eventId: DurableID;
  scope: ActivityScope;
  cursor: CursorString;
  kind: string;
  occurredAt: string;
  actor?: ActivityActor;
  projectId?: DurableID;
  projectRevision?: RevisionString;
  changedEntityRefs: readonly ChangedEntityRef[];
  outcome?: ActivityOutcomeRef;
  summaryCode: string;
}>;

export type ProjectState = Readonly<{
  status: "connecting" | "ready" | "unavailable";
  activityCursor: CursorString;
  projects: readonly Project[];
  error?: Error;
}>;

export interface ProjectReadPort {
  list(signal?: AbortSignal): Promise<ProjectSnapshot>;
  show(id: DurableID, signal?: AbortSignal): Promise<ProjectOverview>;
  getSnapshot(): ProjectState;
  subscribe(listener: () => void): () => void;
}

export interface ProjectWritePort {
  create(input: CreateProjectInput, signal?: AbortSignal): Promise<ProjectCreated>;
}

type ProjectEvents = {
  snapshot: ProjectSnapshot;
  activity: ActivityEvent;
};

export type ProjectPorts = Readonly<{
  read: ProjectReadPort;
  write: ProjectWritePort;
  versions: ProjectVersionPort;
}>;

export type Contracts = Readonly<{
  agent: AgentBridgePort;
  product: ProductStatusPort;
  resources: ProductResourcePort;
  projects: ProjectPorts;
  editing: EditingPorts;
  media: MediaPorts;
  exports: SequenceExportPort;
  authorization: AuthorizationPort;
  start(): void;
  close(): void;
}>;

const zeroCursor = cursorString("0");
const initialState: ProjectState = { status: "connecting", activityCursor: zeroCursor, projects: [] };

export function createContracts(): Contracts {
  const events = new EventBus<ProjectEvents>();
  const media = createMediaPorts(watchProjectActivity);
  const exports = createSequenceExportPort(watchProjectActivity);
  const agent = createAgentBridgePort();
  const listeners = new Set<() => void>();
  let state = initialState;
  let stream: AbortController | undefined;
  let started = false;

  const update = (next: ProjectState): void => {
    state = next;
    for (const listener of listeners) listener();
  };
  const applySnapshot = (snapshot: ProjectSnapshot): void => {
    update({ status: "ready", activityCursor: snapshot.activityCursor, projects: snapshot.projects });
  };
  events.subscribe("snapshot", applySnapshot);

  const read: ProjectReadPort = {
    list: async (signal) => {
      const response = await listProjects(undefined, { signal });
      if (response.status !== 200) throw new Error(`list projects returned ${response.status}`);
      const snapshot = normalizeSnapshot(response.data);
      events.publish("snapshot", snapshot);
      return snapshot;
    },
    show: async (id, signal) => {
      const response = await showProject(id, { signal });
      if (response.status !== 200) throw new Error(`show project returned ${response.status}`);
      return normalizeOverview(response.data);
    },
    getSnapshot: () => state,
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };

  const write: ProjectWritePort = {
    create: async (input, signal) => {
      const response = await createProject(normalizeCreateInput(input), { signal });
      if (response.status !== 200) throw new Error(`create project returned ${response.status}`);
      const created = normalizeCreated(response.data);
      await read.list(signal);
      return created;
    },
  };

  const reconcileActivity = async (event: ActivityEvent, signal: AbortSignal): Promise<void> => {
    if (event.cursor !== incrementCursor(state.activityCursor)) {
      await read.list(signal);
      notifyMediaActivity(media, event);
      return;
    }
    if (event.kind === "workspace.project-created") {
      await read.list(signal);
      return;
    }
    update({ ...state, activityCursor: event.cursor });
    notifyMediaActivity(media, event);
  };

  const runStream = async (signal: AbortSignal, after: CursorString): Promise<void> => {
    let retry = 1000;
    let cursor = after;
    while (!signal.aborted) {
      try {
        const response = await fetch(getWatchActivityUrl({ after: cursor }), {
          headers: { accept: "text/event-stream" },
          signal,
        });
        for await (const message of readServerEvents(response)) {
          if (message.retry !== undefined) retry = message.retry;
          if (message.event !== "activity") continue;
          const event = normalizeActivity(message.data);
          events.publish("activity", event);
          await reconcileActivity(event, signal);
          cursor = state.activityCursor;
        }
        if (!signal.aborted) throw new Error("activity stream ended");
      } catch (error) {
        if (signal.aborted) return;
        update({ ...state, status: "unavailable", error: asError(error) });
      }
      await abortableDelay(retry, signal);
      cursor = state.activityCursor;
    }
  };

  return {
    agent,
    product: createProductStatusPort(),
    resources: createProductResourcePort(),
    projects: { read, write, versions: createProjectVersionPort() },
    editing: createEditingPorts(),
    media,
    exports,
    authorization: createAuthorizationPort(),
    start: () => {
      if (started) return;
      started = true;
      const controller = new AbortController();
      stream = controller;
      update({ ...state, status: "connecting", error: undefined });
      void read
        .list(controller.signal)
        .then((snapshot) => runStream(controller.signal, snapshot.activityCursor))
        .catch((error: unknown) => {
          if (!controller.signal.aborted && stream === controller) {
            update({ ...state, status: "unavailable", error: asError(error) });
          }
        });
    },
    close: () => {
      started = false;
      stream?.abort();
      stream = undefined;
      media.close();
      exports.close();
    },
  };
}

function notifyMediaActivity(media: ReturnType<typeof createMediaPorts>, event: ActivityEvent): void {
  if (
    !event.projectId ||
    (!event.kind.startsWith("asset.") &&
      !event.kind.startsWith("media.") &&
      event.kind !== "workspace.asset-registered")
  )
    return;
  media.notifyProjectChanged(event.projectId);
}

function watchProjectActivity(projectId: DurableID, after: CursorString, invalidate: () => void): () => void {
  const controller = new AbortController();
  void runProjectActivityStream(controller.signal, projectId, after, invalidate);
  return () => controller.abort();
}

async function runProjectActivityStream(
  signal: AbortSignal,
  projectId: DurableID,
  after: CursorString,
  invalidate: () => void,
): Promise<void> {
  let retry = 1000;
  let cursor = after;
  while (!signal.aborted) {
    try {
      const response = await fetch(getWatchActivityUrl({ projectId, after: cursor }), {
        headers: { accept: "text/event-stream" },
        signal,
      });
      for await (const message of readServerEvents(response)) {
        if (message.retry !== undefined) retry = message.retry;
        if (message.event !== "activity") continue;
        const event = normalizeActivity(message.data);
        if (event.scope.kind !== "project" || event.scope.id !== projectId) {
          throw new Error("project activity stream crossed its scope");
        }
        invalidate();
        cursor = event.cursor;
      }
      if (!signal.aborted) throw new Error("project activity stream ended");
    } catch {
      if (signal.aborted) return;
    }
    await abortableDelay(retry, signal);
  }
}

function normalizeSnapshot(value: unknown): ProjectSnapshot {
  const candidate = asRecord(value);
  if (!Array.isArray(candidate.projects)) throw new Error("project snapshot has invalid projects");
  const projects = candidate.projects.map(normalizeProject);
  projects.sort((left, right) => left.id.localeCompare(right.id));
  return {
    activityCursor: cursorString(candidate.activityCursor),
    projects,
    ...(typeof candidate.nextAfter === "string" ? { nextAfter: candidate.nextAfter } : {}),
  };
}

function normalizeCreated(value: unknown): ProjectCreated {
  const candidate = asRecord(value);
  if (typeof candidate.replayed !== "boolean") throw new Error("project creation receipt is invalid");
  return {
    project: normalizeOverview(candidate.project),
    proposalId: durableID(candidate.proposalId),
    transactionId: durableID(candidate.transactionId),
    requestDigest: digestString(candidate.requestDigest),
    projectActivityCursor: cursorString(candidate.projectActivityCursor),
    installationActivityCursor: cursorString(candidate.installationActivityCursor),
    replayed: candidate.replayed,
  };
}

function normalizeOverview(value: unknown): ProjectOverview {
  const candidate = asRecord(value);
  if (!Array.isArray(candidate.tracks)) throw new Error("project overview has invalid tracks");
  return {
    project: normalizeProject(candidate.project),
    narrativeDocumentRevision: revisionString(candidate.narrativeDocumentRevision),
    narrativeRootNodeId: durableID(candidate.narrativeRootNodeId),
    mainSequenceRevision: revisionString(candidate.mainSequenceRevision),
    format: normalizeFormat(candidate.format),
    tracks: candidate.tracks.map(normalizeTrack),
    activityCursor: cursorString(candidate.activityCursor),
  };
}

function normalizeProject(value: unknown): Project {
  const project = asRecord(value);
  if (
    typeof project.name !== "string" ||
    (project.status !== "active" && project.status !== "archived" && project.status !== "tombstoned")
  ) {
    throw new Error("project payload is invalid");
  }
  return {
    id: durableID(project.id),
    revision: revisionString(project.revision),
    lifecycleRevision: revisionString(project.lifecycleRevision),
    name: project.name,
    status: project.status,
    narrativeDocumentId: durableID(project.narrativeDocumentId),
    mainSequenceId: durableID(project.mainSequenceId),
  };
}

function normalizeTrack(value: unknown): Track {
  const track = asRecord(value);
  if (
    typeof track.label !== "string" ||
    (track.type !== "video" && track.type !== "audio" && track.type !== "caption")
  ) {
    throw new Error("track payload is invalid");
  }
  return { id: durableID(track.id), revision: revisionString(track.revision), type: track.type, label: track.label };
}

function normalizeFormat(value: unknown): SequenceFormat {
  const format = asRecord(value);
  if (
    !isBoundedInteger(format.canvasWidth, 16, 16384) ||
    !isBoundedInteger(format.canvasHeight, 16, 16384) ||
    !isBoundedInteger(format.audioSampleRate, 8000, 384000) ||
    format.audioLayout !== "stereo" ||
    format.colorPolicy !== "sdr-rec709"
  ) {
    throw new Error("sequence format is invalid");
  }
  return {
    canvasWidth: format.canvasWidth,
    canvasHeight: format.canvasHeight,
    pixelAspect: normalizeRational(format.pixelAspect),
    frameRate: normalizeRational(format.frameRate),
    audioSampleRate: format.audioSampleRate,
    audioLayout: format.audioLayout,
    colorPolicy: format.colorPolicy,
  };
}

function normalizeRational(value: unknown): RationalTime {
  const rational = asRecord(value);
  if (!isBoundedInteger(rational.scale, 1, 2_147_483_647)) throw new Error("rational scale is invalid");
  const numerator = int64String(rational.value);
  const divisor = greatestCommonDivisor(BigInt(numerator), BigInt(rational.scale));
  if ((numerator === "0" && rational.scale !== 1) || (numerator !== "0" && divisor !== 1n)) {
    throw new Error("rational value is not normalized");
  }
  return { value: numerator, scale: rational.scale };
}

function normalizeActivity(value: unknown): ActivityEvent {
  const event = asRecord(value);
  if (
    event.schema !== "open-cut/activity/v1" ||
    typeof event.kind !== "string" ||
    typeof event.summaryCode !== "string" ||
    typeof event.occurredAt !== "string" ||
    Number.isNaN(Date.parse(event.occurredAt)) ||
    !Array.isArray(event.changedEntityRefs)
  ) {
    throw new Error("activity event is invalid");
  }
  return {
    schema: event.schema,
    eventId: durableID(event.eventId),
    scope: normalizeActivityScope(event.scope),
    cursor: cursorString(event.cursor),
    kind: event.kind,
    occurredAt: event.occurredAt,
    ...(event.actor === undefined ? {} : { actor: normalizeActivityActor(event.actor) }),
    ...(event.projectId === undefined ? {} : { projectId: durableID(event.projectId) }),
    ...(event.projectRevision === undefined ? {} : { projectRevision: revisionString(event.projectRevision) }),
    changedEntityRefs: event.changedEntityRefs.map(normalizeChangedEntityRef),
    ...(event.outcome === undefined ? {} : { outcome: normalizeActivityOutcome(event.outcome) }),
    summaryCode: event.summaryCode,
  };
}

function normalizeCreateInput(input: CreateProjectInput): CreateProjectInput {
  if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(input.requestId)) {
    throw new Error("project request identity is invalid");
  }
  const name = input.name.trim();
  if (name.length < 1 || name.length > 200) throw new Error("project name is invalid");
  return {
    requestId: input.requestId,
    name,
    ...(input.format === undefined ? {} : { format: normalizeFormat(input.format) }),
  };
}

function normalizeActivityScope(value: unknown): ActivityScope {
  const scope = asRecord(value);
  if (
    (scope.kind !== "project" && scope.kind !== "installation") ||
    typeof scope.id !== "string" ||
    scope.id.length < 1 ||
    scope.id.length > 128
  ) {
    throw new Error("activity scope is invalid");
  }
  if (scope.kind === "project") durableID(scope.id);
  return { kind: scope.kind, id: scope.id };
}

function normalizeActivityActor(value: unknown): ActivityActor {
  const actor = asRecord(value);
  if (actor.kind !== "creator" && actor.kind !== "agent") throw new Error("activity actor is invalid");
  return { kind: actor.kind, id: durableID(actor.id) };
}

function normalizeChangedEntityRef(value: unknown): ChangedEntityRef {
  const reference = asRecord(value);
  if (typeof reference.kind !== "string" || reference.kind.length === 0) {
    throw new Error("changed entity reference is invalid");
  }
  return {
    kind: reference.kind,
    id: durableID(reference.id),
    revision: revisionString(reference.revision),
  };
}

function normalizeActivityOutcome(value: unknown): ActivityOutcomeRef {
  const outcome = asRecord(value);
  if (
    typeof outcome.kind !== "string" ||
    outcome.kind.length === 0 ||
    typeof outcome.id !== "string" ||
    outcome.id.length === 0
  ) {
    throw new Error("activity outcome is invalid");
  }
  return { kind: outcome.kind, id: outcome.id };
}

function greatestCommonDivisor(left: bigint, right: bigint): bigint {
  let a = left < 0n ? -left : left;
  let b = right < 0n ? -right : right;
  while (b !== 0n) [a, b] = [b, a % b];
  return a;
}

function isBoundedInteger(value: unknown, minimum: number, maximum: number): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= minimum && value <= maximum;
}

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("product payload is invalid");
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
