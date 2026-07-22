import { createProjectVersion, listProjectVersions, restoreProjectVersion } from "@open-cut/openapi/creator";

import { asRecord } from "./editing-exact.js";
import {
  type CursorString,
  cursorString,
  type DigestString,
  type DurableID,
  digestString,
  durableID,
  type RevisionString,
  revisionString,
  type UInt64String,
  uint64String,
} from "./exact.js";
import { responseError, timestamp } from "./media-validation.js";

export type ProjectVersionSource = "genesis" | "manual" | "agent-turn" | "pre-restore";
export type ProjectVersionRetention = "automatic" | "manual" | "pinned";

export type ProjectVersion = Readonly<{
  id: DurableID;
  projectId: DurableID;
  parentVersionId?: DurableID;
  capturedProjectRevision: RevisionString;
  source: ProjectVersionSource;
  name?: string;
  triggerKind?: "turn" | "version";
  triggerId?: DurableID;
  digest: DigestString;
  byteSize: UInt64String;
  retention: ProjectVersionRetention;
  createdAt: string;
}>;

export type ProjectVersionPage = Readonly<{
  versions: readonly ProjectVersion[];
  nextBefore?: DurableID;
  activityCursor: CursorString;
}>;

export type ListProjectVersionsInput = Readonly<{
  before?: DurableID;
  limit?: number;
}>;

export type CreateProjectVersionInput = Readonly<{
  projectId: DurableID;
  requestId: string;
  name: string;
}>;

export type ProjectVersionCreated = Readonly<{
  version: ProjectVersion;
  activityCursor: CursorString;
  replayed: boolean;
}>;

export type RestoreProjectVersionInput = Readonly<{
  projectId: DurableID;
  versionId: DurableID;
  requestId: string;
  expectedProjectRevision: RevisionString;
}>;

export type ProjectVersionRestored = Readonly<{
  version: ProjectVersion;
  safetyVersion: ProjectVersion;
  transactionId: DurableID;
  committedProjectRevision: RevisionString;
  activityCursor: CursorString;
  replayed: boolean;
}>;

export interface ProjectVersionPort {
  list(projectId: DurableID, input?: ListProjectVersionsInput, signal?: AbortSignal): Promise<ProjectVersionPage>;
  create(input: CreateProjectVersionInput, signal?: AbortSignal): Promise<ProjectVersionCreated>;
  restore(input: RestoreProjectVersionInput, signal?: AbortSignal): Promise<ProjectVersionRestored>;
}

export function createProjectVersionPort(): ProjectVersionPort {
  return {
    list: async (projectId, input = {}, signal) => {
      const normalizedProjectID = durableID(projectId);
      const before = input.before === undefined ? undefined : durableID(input.before);
      if (input.limit !== undefined && (!Number.isInteger(input.limit) || input.limit < 1 || input.limit > 50)) {
        throw new Error("Project version limit is invalid");
      }
      const response = await listProjectVersions(
        normalizedProjectID,
        {
          ...(before === undefined ? {} : { before }),
          ...(input.limit === undefined ? {} : { limit: input.limit }),
        },
        { signal },
      );
      if (response.status !== 200) throw await responseError("list project versions", response.status, response.data);
      return normalizePage(response.data, normalizedProjectID, before);
    },
    create: async (input, signal) => {
      const projectId = durableID(input.projectId);
      const requestId = normalizeRequestID(input.requestId);
      const name = input.name.trim();
      if (name.length < 1 || [...name].length > 200) throw new Error("Project version name is invalid");
      const response = await createProjectVersion(projectId, { requestId, name }, { signal });
      if (response.status !== 200) throw await responseError("create project version", response.status, response.data);
      const result = asRecord(response.data);
      return {
        version: normalizeVersion(result.version, projectId),
        activityCursor: cursorString(result.activityCursor),
        replayed: boolean(result.replayed, "Project version replay state"),
      };
    },
    restore: async (input, signal) => {
      const projectId = durableID(input.projectId);
      const versionId = durableID(input.versionId);
      const response = await restoreProjectVersion(
        projectId,
        versionId,
        {
          requestId: normalizeRequestID(input.requestId),
          expectedProjectRevision: revisionString(input.expectedProjectRevision),
        },
        { signal },
      );
      if (response.status !== 200) throw await responseError("restore project version", response.status, response.data);
      const result = asRecord(response.data);
      const version = normalizeVersion(result.version, projectId);
      if (version.id !== versionId) throw new Error("Restored project version identity is invalid");
      return {
        version,
        safetyVersion: normalizeVersion(result.safetyVersion, projectId),
        transactionId: durableID(result.transactionId),
        committedProjectRevision: revisionString(result.committedProjectRevision),
        activityCursor: cursorString(result.activityCursor),
        replayed: boolean(result.replayed, "Project version replay state"),
      };
    },
  };
}

function normalizePage(value: unknown, projectId: DurableID, before: DurableID | undefined): ProjectVersionPage {
  const page = asRecord(value);
  if (!Array.isArray(page.versions) || page.versions.length > 50) {
    throw new Error("Project versions are invalid");
  }
  const seen = new Set<string>();
  const versions = page.versions.map((entry) => {
    const version = normalizeVersion(entry, projectId);
    if (seen.has(version.id)) throw new Error("Project versions contain duplicates");
    seen.add(version.id);
    return version;
  });
  const nextBefore = page.nextBefore === undefined ? undefined : durableID(page.nextBefore);
  if (
    nextBefore !== undefined &&
    (versions.length === 0 || nextBefore !== versions.at(-1)?.id || nextBefore === before)
  ) {
    throw new Error("Project version continuation is invalid");
  }
  return {
    versions,
    ...(nextBefore === undefined ? {} : { nextBefore }),
    activityCursor: cursorString(page.activityCursor),
  };
}

function normalizeVersion(value: unknown, expectedProjectId: DurableID): ProjectVersion {
  const version = asRecord(value);
  const projectId = durableID(version.projectId);
  if (projectId !== expectedProjectId) throw new Error("Project version belongs to another project");
  const source = projectVersionSource(version.source);
  const retention = projectVersionRetention(version.retention);
  const name = optionalName(version.name);
  const triggerKind = version.triggerKind;
  const triggerId = version.triggerId;
  if ((triggerKind === undefined) !== (triggerId === undefined)) {
    throw new Error("Project version trigger is incomplete");
  }
  if (triggerKind !== undefined && triggerKind !== "turn" && triggerKind !== "version") {
    throw new Error("Project version trigger kind is invalid");
  }
  return {
    id: durableID(version.id),
    projectId,
    ...(version.parentVersionId === undefined ? {} : { parentVersionId: durableID(version.parentVersionId) }),
    capturedProjectRevision: revisionString(version.capturedProjectRevision),
    source,
    ...(name === undefined ? {} : { name }),
    ...(triggerKind === undefined ? {} : { triggerKind, triggerId: durableID(triggerId) }),
    digest: digestString(version.digest),
    byteSize: uint64String(version.byteSize),
    retention,
    createdAt: timestamp(version.createdAt),
  };
}

function projectVersionSource(value: unknown): ProjectVersionSource {
  if (value !== "genesis" && value !== "manual" && value !== "agent-turn" && value !== "pre-restore") {
    throw new Error("Project version source is invalid");
  }
  return value;
}

function projectVersionRetention(value: unknown): ProjectVersionRetention {
  if (value !== "automatic" && value !== "manual" && value !== "pinned") {
    throw new Error("Project version retention is invalid");
  }
  return value;
}

function optionalName(value: unknown): string | undefined {
  if (value === undefined) return undefined;
  if (typeof value !== "string" || value.length < 1 || [...value].length > 200) {
    throw new Error("Project version name is invalid");
  }
  return value;
}

function normalizeRequestID(value: string): string {
  if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(value)) {
    throw new Error("Project version request identity is invalid");
  }
  return value;
}

function boolean(value: unknown, label: string): boolean {
  if (typeof value !== "boolean") throw new Error(`${label} is invalid`);
  return value;
}
