import {
  cancelCreatorSequenceExport,
  deleteCreatorSequenceExportArtifact,
  listCreatorSequenceExports,
  retryCreatorSequenceExport,
  showCreatorSequenceExport,
  startCreatorSequenceExport,
} from "@open-cut/openapi/exports";

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
import {
  asRecord,
  isBoundedInteger,
  normalizeRational,
  readJSON,
  responseError,
  timestamp,
} from "./media-validation.js";
import type { RationalTime } from "./projects.js";

const exportSavePath = "/_open-cut/platform/export-save-as";
const exportRevealPath = "/_open-cut/platform/export-reveal";
const destinationGrantPattern = /^destination\.[A-Za-z0-9_-]{32}$/;
const deliveryReceiptPattern = /^delivery\.[A-Za-z0-9_-]{32}$/;
const exportHistoryCursorPattern = /^export-root\.v1\.[A-Za-z0-9_-]+$/;

export type ExportRecoveryAction =
  | "retry-job"
  | "relink-source"
  | "acquire-resource"
  | "adopt-revision"
  | "update-runtime"
  | "none";

export type SequenceExportJob = Readonly<{
  id: DurableID;
  rootJobId: DurableID;
  retryOfJobId?: DurableID;
  state: "blocked" | "queued" | "running" | "succeeded" | "failed" | "cancelled";
  progressBasisPoints: number;
  terminalErrorCode?: string;
  createdAt: string;
  updatedAt: string;
}>;

export type SequenceExportArtifact = Readonly<{
  id: DurableID;
  verification: "passed";
  semanticDuration: RationalTime;
  presentationDuration: RationalTime;
  canvasWidth: number;
  canvasHeight: number;
  frameRate: RationalTime;
  videoFrameCount: UInt64String;
  audioSampleRate: 48_000;
  audioSampleCount: UInt64String;
  videoCodec: "vp9";
  audioCodec: "opus";
  pixelFormat: "yuv420p";
  channelLayout: "stereo";
  byteSize: UInt64String;
  contentDigest: DigestString;
}>;

export type SequenceExport = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  sequenceRevision: RevisionString;
  preset: "webm-vp9-opus-v1";
  job: SequenceExportJob;
  artifact?: SequenceExportArtifact;
  recovery: ExportRecoveryAction;
  replayed: boolean;
  activityCursor: CursorString;
}>;

export type StartSequenceExportInput = Readonly<{
  requestId: string;
  sequenceRevision: RevisionString;
  preset: "webm-vp9-opus-v1";
}>;

export type SequenceExportLineage = Readonly<{
  origin: "agent" | "creator";
  attemptCount: UInt64String;
  artifactAvailability: "none" | "ready" | "invalid" | "deleted";
  rootCreatedAt: string;
  export: SequenceExport;
}>;

export type SequenceExportHistoryPage = Readonly<{
  lineages: readonly SequenceExportLineage[];
  nextAfter?: string;
  activityCursor: CursorString;
}>;

export type ListSequenceExportHistoryInput = Readonly<{
  after?: string;
  limit?: number;
}>;

export type ExportSaveInput = Readonly<{
  projectId: DurableID;
  artifactId: DurableID;
  suggestedName: string;
  destinationGrant?: string;
  overwrite?: true;
}>;

export type ExportSaveResult =
  | Readonly<{ status: "cancelled" }>
  | Readonly<{
      status: "overwrite-required";
      destinationGrant: string;
      displayName: string;
    }>
  | Readonly<{
      status: "saved";
      displayName: string;
      byteLength: UInt64String;
      contentSha256: DigestString;
      deliveryReceipt: string;
    }>;

export type ExportRevealResult = Readonly<{
  status: "revealed";
  displayName: string;
}>;

export interface SequenceExportPort {
  list(
    projectId: DurableID,
    input?: ListSequenceExportHistoryInput,
    signal?: AbortSignal,
  ): Promise<SequenceExportHistoryPage>;
  start(
    projectId: DurableID,
    sequenceId: DurableID,
    input: StartSequenceExportInput,
    signal?: AbortSignal,
  ): Promise<SequenceExport>;
  show(projectId: DurableID, jobId: DurableID, signal?: AbortSignal): Promise<SequenceExport>;
  retry(projectId: DurableID, jobId: DurableID, signal?: AbortSignal): Promise<SequenceExport>;
  cancel(projectId: DurableID, jobId: DurableID, requestId: string, signal?: AbortSignal): Promise<SequenceExport>;
  deleteArtifact(
    projectId: DurableID,
    jobId: DurableID,
    artifactId: DurableID,
    requestId: string,
    signal?: AbortSignal,
  ): Promise<SequenceExport>;
  saveAs(input: ExportSaveInput, signal?: AbortSignal): Promise<ExportSaveResult>;
  reveal(deliveryReceipt: string, signal?: AbortSignal): Promise<ExportRevealResult>;
  subscribe(projectId: DurableID, listener: () => void): () => void;
}

export type SequenceExportRuntimePort = SequenceExportPort & Readonly<{ close(): void }>;

export type ExportActivityWatcher = (projectId: DurableID, after: CursorString, invalidate: () => void) => () => void;

export function createSequenceExportPort(watchProjectActivity?: ExportActivityWatcher): SequenceExportRuntimePort {
  const cursors = new Map<DurableID, CursorString>();
  const listeners = new Map<DurableID, Set<() => void>>();
  const watches = new Map<DurableID, () => void>();
  const notify = (projectId: DurableID): void => {
    for (const listener of listeners.get(projectId) ?? []) listener();
  };
  const ensureWatch = (projectId: DurableID): void => {
    if (!watchProjectActivity || watches.has(projectId) || !listeners.has(projectId)) return;
    const cursor = cursors.get(projectId);
    if (!cursor) return;
    watches.set(
      projectId,
      watchProjectActivity(projectId, cursor, () => notify(projectId)),
    );
  };
  const accept = (value: unknown): SequenceExport => {
    const result = normalizeSequenceExport(value);
    cursors.set(result.projectId, result.activityCursor);
    ensureWatch(result.projectId);
    return result;
  };
  const acceptHistory = (projectId: DurableID, value: unknown): SequenceExportHistoryPage => {
    const result = normalizeSequenceExportHistory(value);
    for (const lineage of result.lineages) {
      if (lineage.export.projectId !== projectId) throw new Error("Sequence export history project is invalid");
    }
    cursors.set(projectId, result.activityCursor);
    ensureWatch(projectId);
    return result;
  };
  return {
    list: async (projectId, input = {}, signal) => {
      if (
        (input.limit !== undefined && !isBoundedInteger(input.limit, 1, 50)) ||
        (input.after !== undefined && (input.after.length > 512 || !exportHistoryCursorPattern.test(input.after)))
      )
        throw new Error("Sequence export history input is invalid");
      const response = await listCreatorSequenceExports(projectId, input, { signal });
      if (response.status !== 200) {
        throw await responseError("list Sequence exports", response.status, response.data);
      }
      return acceptHistory(projectId, response.data);
    },
    start: async (projectId, sequenceId, input, signal) => {
      const response = await startCreatorSequenceExport(
        projectId,
        sequenceId,
        {
          requestId: input.requestId,
          sequenceRevision: revisionString(input.sequenceRevision),
          preset: input.preset,
        },
        { signal },
      );
      if (response.status !== 200) {
        throw await responseError("start Sequence export", response.status, response.data);
      }
      return accept(response.data);
    },
    show: async (projectId, jobId, signal) => {
      const response = await showCreatorSequenceExport(projectId, jobId, { signal });
      if (response.status !== 200) {
        throw await responseError("show Sequence export", response.status, response.data);
      }
      return accept(response.data);
    },
    retry: async (projectId, jobId, signal) => {
      const response = await retryCreatorSequenceExport(projectId, jobId, { signal });
      if (response.status !== 200) {
        throw await responseError("retry Sequence export", response.status, response.data);
      }
      return accept(response.data);
    },
    cancel: async (projectId, jobId, requestId, signal) => {
      const response = await cancelCreatorSequenceExport(projectId, jobId, { requestId }, { signal });
      if (response.status !== 200) {
        throw await responseError("cancel Sequence export", response.status, response.data);
      }
      return accept(response.data);
    },
    deleteArtifact: async (projectId, jobId, artifactId, requestId, signal) => {
      const response = await deleteCreatorSequenceExportArtifact(
        projectId,
        jobId,
        { artifactId, requestId },
        { signal },
      );
      if (response.status !== 200) {
        throw await responseError("delete Sequence export artifact", response.status, response.data);
      }
      return accept(response.data);
    },
    saveAs: saveExportAs,
    reveal: revealExport,
    subscribe: (projectId, listener) => {
      let current = listeners.get(projectId);
      if (!current) {
        current = new Set();
        listeners.set(projectId, current);
      }
      current.add(listener);
      ensureWatch(projectId);
      return () => {
        current?.delete(listener);
        if (current?.size === 0) {
          listeners.delete(projectId);
          watches.get(projectId)?.();
          watches.delete(projectId);
        }
      };
    },
    close: () => {
      for (const close of watches.values()) close();
      watches.clear();
      listeners.clear();
      cursors.clear();
    },
  };
}

function normalizeSequenceExportHistory(value: unknown): SequenceExportHistoryPage {
  const page = asRecord(value);
  if (!Array.isArray(page.lineages) || page.lineages.length > 50) {
    throw new Error("Sequence export history page is invalid");
  }
  if (
    page.nextAfter !== undefined &&
    (typeof page.nextAfter !== "string" ||
      page.nextAfter.length === 0 ||
      page.nextAfter.length > 512 ||
      !exportHistoryCursorPattern.test(page.nextAfter))
  )
    throw new Error("Sequence export history cursor is invalid");
  return {
    lineages: page.lineages.map(normalizeSequenceExportLineage),
    ...(typeof page.nextAfter === "string" ? { nextAfter: page.nextAfter } : {}),
    activityCursor: cursorString(page.activityCursor),
  };
}

function normalizeSequenceExportLineage(value: unknown): SequenceExportLineage {
  const lineage = asRecord(value);
  if (lineage.origin !== "agent" && lineage.origin !== "creator") {
    throw new Error("Sequence export origin is invalid");
  }
  if (
    lineage.artifactAvailability !== "none" &&
    lineage.artifactAvailability !== "ready" &&
    lineage.artifactAvailability !== "invalid" &&
    lineage.artifactAvailability !== "deleted"
  )
    throw new Error("Sequence export artifact availability is invalid");
  const attemptCount = uint64String(lineage.attemptCount);
  if (attemptCount === "0") throw new Error("Sequence export attempt count is invalid");
  const exported = normalizeSequenceExport(lineage.export);
  const available = lineage.artifactAvailability;
  if (
    (available === "ready" && !exported.artifact) ||
    (available !== "ready" && exported.artifact !== undefined) ||
    ((available === "invalid" || available === "deleted") &&
      (exported.job.state !== "succeeded" || exported.recovery !== "retry-job")) ||
    (available === "none" && exported.job.state === "succeeded")
  )
    throw new Error("Sequence export lineage state is invalid");
  return {
    origin: lineage.origin,
    attemptCount,
    artifactAvailability: available,
    rootCreatedAt: timestamp(lineage.rootCreatedAt),
    export: exported,
  };
}

function normalizeSequenceExport(value: unknown): SequenceExport {
  const result = asRecord(value);
  const job = normalizeExportJob(result.job);
  const recovery = normalizeRecovery(result.recovery);
  if (result.preset !== "webm-vp9-opus-v1" || typeof result.replayed !== "boolean") {
    throw new Error("Sequence export projection is invalid");
  }
  const artifact = result.artifact === undefined ? undefined : normalizeExportArtifact(result.artifact);
  if ((job.state === "succeeded" && !artifact && recovery !== "retry-job") || (job.state !== "succeeded" && artifact)) {
    throw new Error("Sequence export artifact state is invalid");
  }
  return {
    projectId: durableID(result.projectId),
    sequenceId: durableID(result.sequenceId),
    sequenceRevision: revisionString(result.sequenceRevision),
    preset: result.preset,
    job,
    ...(artifact ? { artifact } : {}),
    recovery,
    replayed: result.replayed,
    activityCursor: cursorString(result.activityCursor),
  };
}

function normalizeExportJob(value: unknown): SequenceExportJob {
  const job = asRecord(value);
  if (
    !isExportState(job.state) ||
    !isBoundedInteger(job.progressBasisPoints, 0, 10_000) ||
    (job.terminalErrorCode !== undefined &&
      (typeof job.terminalErrorCode !== "string" ||
        job.terminalErrorCode.length === 0 ||
        job.terminalErrorCode.length > 64))
  )
    throw new Error("Sequence export job is invalid");
  return {
    id: durableID(job.id),
    rootJobId: durableID(job.rootJobId),
    ...(job.retryOfJobId === undefined ? {} : { retryOfJobId: durableID(job.retryOfJobId) }),
    state: job.state,
    progressBasisPoints: job.progressBasisPoints,
    ...(typeof job.terminalErrorCode === "string" ? { terminalErrorCode: job.terminalErrorCode } : {}),
    createdAt: timestamp(job.createdAt),
    updatedAt: timestamp(job.updatedAt),
  };
}

function normalizeExportArtifact(value: unknown): SequenceExportArtifact {
  const artifact = asRecord(value);
  if (
    artifact.verification !== "passed" ||
    !isBoundedInteger(artifact.canvasWidth, 2, 16_384) ||
    !isBoundedInteger(artifact.canvasHeight, 2, 16_384) ||
    artifact.audioSampleRate !== 48_000 ||
    artifact.videoCodec !== "vp9" ||
    artifact.audioCodec !== "opus" ||
    artifact.pixelFormat !== "yuv420p" ||
    artifact.channelLayout !== "stereo"
  )
    throw new Error("Sequence export artifact is invalid");
  return {
    id: durableID(artifact.id),
    verification: artifact.verification,
    semanticDuration: normalizeRational(artifact.semanticDuration),
    presentationDuration: normalizeRational(artifact.presentationDuration),
    canvasWidth: artifact.canvasWidth,
    canvasHeight: artifact.canvasHeight,
    frameRate: normalizeRational(artifact.frameRate),
    videoFrameCount: uint64String(artifact.videoFrameCount),
    audioSampleRate: artifact.audioSampleRate,
    audioSampleCount: uint64String(artifact.audioSampleCount),
    videoCodec: artifact.videoCodec,
    audioCodec: artifact.audioCodec,
    pixelFormat: artifact.pixelFormat,
    channelLayout: artifact.channelLayout,
    byteSize: uint64String(artifact.byteSize),
    contentDigest: digestString(artifact.contentDigest),
  };
}

async function saveExportAs(input: ExportSaveInput, signal?: AbortSignal): Promise<ExportSaveResult> {
  if (!validSuggestedName(input.suggestedName)) throw new Error("Export suggested name is invalid");
  if (
    input.destinationGrant !== undefined &&
    (!destinationGrantPattern.test(input.destinationGrant) || input.overwrite !== true)
  )
    throw new Error("Export destination grant is invalid");
  if (input.destinationGrant === undefined && input.overwrite !== undefined) {
    throw new Error("Export overwrite requires a destination grant");
  }
  const response = await fetch(exportSavePath, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(input),
    signal,
  });
  if (response.status === 204) return { status: "cancelled" };
  const data = await readJSON(response);
  const result = asRecord(data);
  if (response.status === 409 && result.error === "OC_EXPORT_OVERWRITE_REQUIRED") {
    if (
      typeof result.destinationGrant !== "string" ||
      !destinationGrantPattern.test(result.destinationGrant) ||
      !validDisplayName(result.displayName)
    )
      throw new Error("Export overwrite receipt is invalid");
    return {
      status: "overwrite-required",
      destinationGrant: result.destinationGrant,
      displayName: result.displayName,
    };
  }
  if (!response.ok) throw await responseError("save Sequence export", response.status, data);
  if (
    result.status !== "saved" ||
    !validDisplayName(result.displayName) ||
    typeof result.deliveryReceipt !== "string" ||
    !deliveryReceiptPattern.test(result.deliveryReceipt)
  ) {
    throw new Error("Export Save As receipt is invalid");
  }
  return {
    status: "saved",
    displayName: result.displayName,
    byteLength: uint64String(result.byteLength),
    contentSha256: digestString(result.contentSha256),
    deliveryReceipt: result.deliveryReceipt,
  };
}

async function revealExport(deliveryReceipt: string, signal?: AbortSignal): Promise<ExportRevealResult> {
  if (!deliveryReceiptPattern.test(deliveryReceipt)) throw new Error("Export delivery receipt is invalid");
  const response = await fetch(exportRevealPath, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ deliveryReceipt }),
    signal,
  });
  const data = await readJSON(response);
  if (!response.ok) throw await responseError("reveal Sequence export", response.status, data);
  const result = asRecord(data);
  if (result.status !== "revealed" || !validDisplayName(result.displayName)) {
    throw new Error("Export reveal result is invalid");
  }
  return { status: "revealed", displayName: result.displayName };
}

function isExportState(value: unknown): value is SequenceExportJob["state"] {
  return (
    value === "blocked" ||
    value === "queued" ||
    value === "running" ||
    value === "succeeded" ||
    value === "failed" ||
    value === "cancelled"
  );
}

function normalizeRecovery(value: unknown): ExportRecoveryAction {
  if (
    value !== "retry-job" &&
    value !== "relink-source" &&
    value !== "acquire-resource" &&
    value !== "adopt-revision" &&
    value !== "update-runtime" &&
    value !== "none"
  )
    throw new Error("Sequence export recovery is invalid");
  return value;
}

function validSuggestedName(value: string): boolean {
  return value.length > 5 && value.length <= 128 && value.endsWith(".webm") && !/[\\/\0\r\n]/.test(value);
}

function validDisplayName(value: unknown): value is string {
  return typeof value === "string" && value.length > 0 && value.length <= 255 && !/[\\/\0\r\n]/.test(value);
}
