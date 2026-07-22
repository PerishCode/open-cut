import {
  createMediaLease,
  createSequencePreviewLease,
  inspectAsset,
  listAssets,
  readTranscript,
  registerAsset,
  resolveSourcePreviewPosition,
  selectDefaultTranscript,
} from "@open-cut/openapi/media";

import {
  type CursorString,
  cursorString,
  type DigestString,
  type DurableID,
  digestString,
  durableID,
  type Int64String,
  type RevisionString,
  revisionString,
  type UInt64String,
} from "./exact.js";
import {
  normalizeAsset,
  normalizeAssetDetail,
  normalizeAssetPage,
  normalizeSequencePreviewPreparation,
  normalizeSourceGrant,
} from "./media-normalization.js";
import { asRecord, normalizeRational, readJSON, responseError, validateLimit } from "./media-validation.js";
import type { RationalTime } from "./projects.js";
import { normalizeSourcePositionResult, normalizeSourcePreviewPreparation } from "./source-media-normalization.js";
import {
  normalizeTranscriptDefaultSelection,
  normalizeTranscriptReadPage,
  transcriptAfter,
  transcriptLimit,
} from "./transcript-normalization.js";

const sourceSelectionPath = "/_open-cut/platform/source-grants";
const droppedSourceTokenPattern = /^drop\.[A-Za-z0-9_-]{32}$/;

export type OpenCutPlatformBridge = Readonly<{
  stageDroppedSource(file: File): Promise<string>;
}>;

declare global {
  interface Window {
    openCutPlatform?: OpenCutPlatformBridge;
  }
}

export type SourceObservation = Readonly<{
  byteSize: UInt64String;
  modifiedUnixNs: Int64String;
  fileIdentity: string;
}>;

export type SourceGrant = Readonly<{
  id: DurableID;
  platform: "mac" | "win" | "linux";
  kind: "local-path-v1" | "mac-security-scoped-bookmark-v1";
  displayName: string;
  observation: SourceObservation;
  state: "active" | "revoked" | "unavailable";
  createdAt: string;
}>;

export type MediaType = "video" | "audio" | "subtitle" | "data" | "attachment" | "other";

export type VideoStreamFacts = Readonly<{
  width: number;
  height: number;
  codedWidth?: number;
  codedHeight?: number;
  pixelAspect?: RationalTime;
  averageRate?: RationalTime;
  nominalRate?: RationalTime;
  rotation: 0 | 90 | 180 | 270;
  pixelFormat?: string;
  colorRange?: string;
  colorSpace?: string;
  colorTransfer?: string;
  colorPrimaries?: string;
}>;

export type AudioStreamFacts = Readonly<{
  sampleFormat?: string;
  sampleRate: number;
  channels: number;
  channelLayout?: string;
}>;

export type SourceStreamDescriptor = Readonly<{
  index: number;
  mediaType: MediaType;
  codec: string;
  codecProfile?: string;
  codecTag?: string;
  timeBase: RationalTime;
  startTime?: RationalTime;
  duration?: RationalTime;
  language?: string;
  dispositions: readonly string[];
  video?: VideoStreamFacts;
  audio?: AudioStreamFacts;
}>;

export type SourceStream = Readonly<{
  id: DurableID;
  descriptor: SourceStreamDescriptor;
}>;

export type MediaFacts = Readonly<{
  container: string;
  containerAliases: readonly string[];
  startTime?: RationalTime;
  duration?: RationalTime;
  bitRate?: UInt64String;
  streams: readonly SourceStream[];
}>;

export type MediaArtifact = Readonly<{
  id: DurableID;
  kind: "media-facts" | "frame-sample-set" | "proxy" | "render-input" | "waveform" | "transcript";
  producerVersion: string;
  inputFingerprint: DigestString;
  state: "ready" | "evicted";
  byteSize: UInt64String;
  contentDigest: DigestString;
  createdAt: string;
}>;

export type MediaJobPrerequisite =
  | Readonly<{ kind: "fingerprint-required" | "facts-required"; jobId: DurableID }>
  | Readonly<{ kind: "model-required"; resourceId: string }>
  | Readonly<{ kind: "executor-required"; capability: string }>;

export type MediaJob = Readonly<{
  id: DurableID;
  kind: "identify" | "probe" | "frame-sample-set" | "proxy" | "render-input" | "waveform" | "transcript";
  state: "blocked" | "queued" | "running" | "succeeded" | "failed" | "cancelled";
  progressBasisPoints: number;
  prerequisites: readonly MediaJobPrerequisite[];
  terminalErrorCode?: string;
  resultArtifactId?: DurableID;
  createdAt: string;
  updatedAt: string;
}>;

export type Asset = Readonly<{
  id: DurableID;
  revision: RevisionString;
  projectId: DurableID;
  displayName: string;
  importMode: "referenced" | "managed";
  acceptedFingerprint?: DigestString;
  tombstoned: boolean;
  availability: "identifying" | "online" | "changed" | "missing" | "managed" | "unreadable";
  fingerprint?: DigestString;
  facts?: MediaFacts;
  artifacts: readonly MediaArtifact[];
  jobs: readonly MediaJob[];
}>;

export type AssetPage = Readonly<{
  assets: readonly Asset[];
  nextAfter?: string;
  activityCursor: CursorString;
}>;

export type AssetListInput = Readonly<{ after?: string; limit?: number }>;

export type TranscriptReadInput = Readonly<{
  artifactId?: DurableID;
  after?: string;
  limit?: number;
}>;

export type TranscriptToken = Readonly<{
  id: DurableID;
  sourceRange: Readonly<{ start: RationalTime; duration: RationalTime }>;
  text: string;
  confidenceBasisPoints?: number;
}>;

export type TranscriptSegment = Readonly<{
  id: DurableID;
  ordinal: number;
  sourceRange: Readonly<{ start: RationalTime; duration: RationalTime }>;
  text: string;
  tokens: readonly TranscriptToken[];
}>;

export type TranscriptArtifact = Readonly<{
  id: DurableID;
  assetId: DurableID;
  sourceStreamId: DurableID;
  recognitionProfile: "whisper-small-multilingual-v1";
  engineVersion: string;
  engineTarget: string;
  modelName: string;
  modelVersion: string;
  detectedLanguage: string;
  languageConfidenceBasisPoints?: number;
  sourceStartTime: RationalTime;
  normalizedSampleCount: UInt64String;
  isDefault: boolean;
  createdAt: string;
}>;

export type TranscriptCorrection = Readonly<{
  id: DurableID;
  revision: RevisionString;
  segmentIds: readonly DurableID[];
  sourceRange: Readonly<{ start: RationalTime; duration: RationalTime }>;
  originalText: string;
  effectiveText: string;
  language: string;
}>;

export type TranscriptReadPage = Readonly<{
  schema: "open-cut/transcript-read/v1";
  artifact: TranscriptArtifact;
  segments: readonly TranscriptSegment[];
  corrections: readonly TranscriptCorrection[];
  nextAfter?: string;
  activityCursor: CursorString;
}>;

export type SelectTranscriptDefaultInput = Readonly<{
  artifactId: DurableID;
  expectedDefaultArtifactId: DurableID;
}>;

export type TranscriptDefaultSelection = Readonly<{
  assetId: DurableID;
  artifactId: DurableID;
  previousArtifactId: DurableID;
  selectedAt: string;
  activityCursor: CursorString;
  replayed: boolean;
}>;

export type ImportReferencedAssetInput = Readonly<{
  projectId: DurableID;
  expectedProjectRevision: RevisionString;
  droppedFile?: File;
}>;

export type AssetImported = Readonly<{
  sourceGrant: SourceGrant;
  asset: Asset;
  transactionId: DurableID;
  committedProjectRevision: RevisionString;
  activityCursor: CursorString;
  replayed: boolean;
}>;

export type SourcePreviewSelectionInput = Readonly<{
  assetRevision: RevisionString;
  fingerprint: DigestString;
  videoStreamId?: DurableID;
  audioStreamId?: DurableID;
}>;

export type SourcePreviewTrackTiming = Readonly<{
  sourceStreamId: DurableID;
  coverageStart: RationalTime;
  coverageDuration?: RationalTime;
  sourceStartTime: RationalTime;
  proxyStartTime: RationalTime;
  sourceTimeBase: RationalTime;
  proxyTimeBase: RationalTime;
}>;

export type SourcePreviewLease = Readonly<{
  schema: "open-cut/media-lease/v1";
  resourceId: DurableID;
  purpose: "source-preview";
  projectId: DurableID;
  assetId: DurableID;
  assetRevision: RevisionString;
  fingerprint: DigestString;
  artifactId: DurableID;
  artifactDigest: DigestString;
  mimeType: "video/webm" | "audio/webm";
  byteLength: UInt64String;
  etag: string;
  sameOriginUrl: string;
  expiresAt: string;
  sourceEpoch: RationalTime;
  video?: SourcePreviewTrackTiming;
  audio?: SourcePreviewTrackTiming;
}>;

export type MediaPreparationStage = "proxy" | "integrity" | "render";

export type MediaRecoveryAction =
  | "automatic-retry"
  | "retry-job"
  | "relink-source"
  | "acquire-resource"
  | "adopt-revision"
  | "update-runtime"
  | "none";

export type MediaDiagnostic = Readonly<{
  code:
    | "source-proxy-integrity-rejected"
    | "source-proxy-job-failed"
    | "source-proxy-job-cancelled"
    | "sequence-preview-integrity-rejected"
    | "sequence-preview-job-failed"
    | "sequence-preview-job-cancelled";
  severity: "degraded" | "blocking";
  subjectKind: "asset" | "media-job" | "work-job" | "artifact";
  subjectId: DurableID;
  recovery: MediaRecoveryAction;
}>;

export type SourcePreviewPreparation = Readonly<{
  status: "ready" | "preparing" | "failed";
  purpose: "source-preview";
  projectId: DurableID;
  assetId: DurableID;
  assetRevision: RevisionString;
  fingerprint: DigestString;
  videoStreamId?: DurableID;
  audioStreamId?: DurableID;
  job: MediaJob;
  stage?: MediaPreparationStage;
  diagnostics: readonly MediaDiagnostic[];
  lease?: SourcePreviewLease;
}>;

export type SourcePositionOperation = "settle" | "previous" | "next";

export type SourcePositionInput = Readonly<{
  resourceId: DurableID;
  operation: SourcePositionOperation;
  target: RationalTime;
}>;

export type SourcePositionResult = Readonly<{
  resourceId: DurableID;
  projectId: DurableID;
  assetId: DurableID;
  assetRevision: RevisionString;
  fingerprint: DigestString;
  videoStreamId?: DurableID;
  audioStreamId?: DurableID;
  operation: SourcePositionOperation;
  requestedTime: RationalTime;
  sourceTime: RationalTime;
  proxyTime: RationalTime;
  boundary: "video-presentation" | "audio-sample" | "coverage-end";
  atStart: boolean;
  atEnd: boolean;
}>;

export type SequencePreviewMediaFacts = Readonly<{
  semanticDuration: RationalTime;
  presentationDuration: RationalTime;
  canvasWidth: number;
  canvasHeight: number;
  frameRate: RationalTime;
  videoFrameCount: UInt64String;
  audioSampleRate: number;
  audioSampleCount: UInt64String;
  videoCodec: "vp9";
  audioCodec: "opus";
  pixelFormat: "yuv420p";
  channelLayout: "stereo";
}>;

export type SequencePreviewJob = Readonly<{
  id: DurableID;
  kind: "sequence-preview";
  state: "blocked" | "queued" | "running" | "succeeded" | "failed" | "cancelled";
  progressBasisPoints: number;
  terminalErrorCode?: string;
  renderPlanDigest?: DigestString;
  resultArtifactId?: DurableID;
  createdAt: string;
  updatedAt: string;
}>;

export type SequencePreviewContinuation = Readonly<{
  jobId: DurableID;
  renderPlanDigest?: DigestString;
}>;

export type SequencePreviewInput = Readonly<{
  expectedSequenceRevision: RevisionString;
}>;

export type SequencePreviewContinuationInput = SequencePreviewInput &
  Readonly<{ continuation: SequencePreviewContinuation }>;

export type SequencePreviewLease = Readonly<{
  schema: "open-cut/media-lease/v1";
  resourceId: DurableID;
  purpose: "sequence-preview";
  projectId: DurableID;
  sequenceId: DurableID;
  sequenceRevision: RevisionString;
  renderPlanDigest: DigestString;
  artifactId: DurableID;
  artifactDigest: DigestString;
  facts: SequencePreviewMediaFacts;
  mimeType: "video/webm";
  byteLength: UInt64String;
  etag: string;
  sameOriginUrl: string;
  expiresAt: string;
}>;

export type SequencePreviewPreparation = Readonly<{
  status: "empty" | "ready" | "preparing" | "failed";
  purpose: "sequence-preview";
  projectId: DurableID;
  sequenceId: DurableID;
  sequenceRevision: RevisionString;
  job?: SequencePreviewJob;
  continuation?: SequencePreviewContinuation;
  stage?: "render" | "integrity";
  diagnostics: readonly MediaDiagnostic[];
  lease?: SequencePreviewLease;
}>;

export interface MediaReadPort {
  list(projectId: DurableID, input?: AssetListInput, signal?: AbortSignal): Promise<AssetPage>;
  inspect(projectId: DurableID, assetId: DurableID, signal?: AbortSignal): Promise<Asset>;
  transcript(
    projectId: DurableID,
    assetId: DurableID,
    input?: TranscriptReadInput,
    signal?: AbortSignal,
  ): Promise<TranscriptReadPage>;
  subscribe(projectId: DurableID, listener: () => void): () => void;
}

export interface MediaWritePort {
  importReferenced(input: ImportReferencedAssetInput, signal?: AbortSignal): Promise<AssetImported | undefined>;
  selectTranscriptDefault(
    projectId: DurableID,
    assetId: DurableID,
    input: SelectTranscriptDefaultInput,
    signal?: AbortSignal,
  ): Promise<TranscriptDefaultSelection>;
}

export interface ViewerMediaPort {
  prepareSourcePreview(
    projectId: DurableID,
    assetId: DurableID,
    input: SourcePreviewSelectionInput,
    signal?: AbortSignal,
  ): Promise<SourcePreviewPreparation>;
  resolveSourcePosition(
    projectId: DurableID,
    assetId: DurableID,
    selection: SourcePreviewSelectionInput,
    input: SourcePositionInput,
    signal?: AbortSignal,
  ): Promise<SourcePositionResult>;
  prepareSequencePreview(
    projectId: DurableID,
    sequenceId: DurableID,
    input: SequencePreviewInput,
    signal?: AbortSignal,
  ): Promise<SequencePreviewPreparation>;
  continueSequencePreview(
    projectId: DurableID,
    sequenceId: DurableID,
    input: SequencePreviewContinuationInput,
    signal?: AbortSignal,
  ): Promise<SequencePreviewPreparation>;
  retrySequencePreview(
    projectId: DurableID,
    sequenceId: DurableID,
    input: SequencePreviewContinuationInput,
    signal?: AbortSignal,
  ): Promise<SequencePreviewPreparation>;
}

export type MediaPorts = Readonly<{ read: MediaReadPort; write: MediaWritePort; viewer: ViewerMediaPort }>;

export type ProjectActivityWatcher = (projectId: DurableID, after: CursorString, invalidate: () => void) => () => void;

export type MediaRuntimePorts = MediaPorts &
  Readonly<{
    notifyProjectChanged(projectId: DurableID): void;
    close(): void;
  }>;

export function createMediaPorts(watchProjectActivity?: ProjectActivityWatcher): MediaRuntimePorts {
  const projectListeners = new Map<DurableID, Set<() => void>>();
  const projectCursors = new Map<DurableID, CursorString>();
  const projectWatches = new Map<DurableID, () => void>();
  const notifyProjectChanged = (projectId: DurableID): void => {
    for (const listener of projectListeners.get(projectId) ?? []) listener();
  };
  const ensureProjectWatch = (projectId: DurableID): void => {
    if (!watchProjectActivity || projectWatches.has(projectId) || !projectListeners.has(projectId)) return;
    const cursor = projectCursors.get(projectId);
    if (!cursor) return;
    projectWatches.set(
      projectId,
      watchProjectActivity(projectId, cursor, () => notifyProjectChanged(projectId)),
    );
  };
  const requestSequencePreview = async (
    operation: "prepare" | "continue" | "retry",
    projectId: DurableID,
    sequenceId: DurableID,
    input: SequencePreviewInput | SequencePreviewContinuationInput,
    signal?: AbortSignal,
  ): Promise<SequencePreviewPreparation> => {
    const expectedSequenceRevision = revisionString(input.expectedSequenceRevision);
    const continuation =
      "continuation" in input
        ? {
            jobId: durableID(input.continuation.jobId),
            ...(input.continuation.renderPlanDigest === undefined
              ? {}
              : { renderPlanDigest: digestString(input.continuation.renderPlanDigest) }),
          }
        : undefined;
    if ((operation === "prepare") !== (continuation === undefined)) {
      throw new Error("sequence preview operation and continuation do not match");
    }
    const response = await createSequencePreviewLease(
      projectId,
      sequenceId,
      {
        purpose: "sequence-preview",
        operation,
        expectedSequenceRevision,
        ...(continuation === undefined ? {} : { continuation }),
      },
      { signal },
    );
    if (response.status !== 200) {
      throw await responseError(`${operation} sequence preview`, response.status, response.data);
    }
    return normalizeSequencePreviewPreparation(response.data, projectId, sequenceId, expectedSequenceRevision);
  };
  return {
    read: {
      list: async (projectId, input, signal) => {
        const response = await listAssets(
          projectId,
          {
            ...(input?.after === undefined ? {} : { after: input.after }),
            ...(input?.limit === undefined ? {} : { limit: validateLimit(input.limit) }),
          },
          { signal },
        );
        if (response.status !== 200) throw await responseError("list Assets", response.status, response.data);
        const page = normalizeAssetPage(response.data);
        projectCursors.set(projectId, page.activityCursor);
        ensureProjectWatch(projectId);
        return page;
      },
      inspect: async (projectId, assetId, signal) => {
        const response = await inspectAsset(projectId, assetId, { signal });
        if (response.status !== 200) throw await responseError("inspect Asset", response.status, response.data);
        const payload = asRecord(response.data);
        cursorString(payload.activityCursor);
        return normalizeAsset(payload.asset);
      },
      transcript: async (projectId, assetId, input, signal) => {
        const after = input?.after === undefined ? undefined : transcriptAfter(input.after);
        const response = await readTranscript(
          projectId,
          assetId,
          {
            ...(input?.artifactId === undefined ? {} : { artifactId: durableID(input.artifactId) }),
            ...(after === undefined ? {} : { after }),
            ...(input?.limit === undefined ? {} : { limit: transcriptLimit(input.limit) }),
          },
          { signal },
        );
        if (response.status !== 200) throw await responseError("read transcript", response.status, response.data);
        const page = normalizeTranscriptReadPage(response.data, assetId, after);
        projectCursors.set(projectId, page.activityCursor);
        ensureProjectWatch(projectId);
        return page;
      },
      subscribe: (projectId, listener) => {
        let listeners = projectListeners.get(projectId);
        if (!listeners) {
          listeners = new Set();
          projectListeners.set(projectId, listeners);
        }
        listeners.add(listener);
        ensureProjectWatch(projectId);
        return () => {
          listeners?.delete(listener);
          if (listeners?.size === 0) {
            projectListeners.delete(projectId);
            projectWatches.get(projectId)?.();
            projectWatches.delete(projectId);
          }
        };
      },
    },
    write: {
      importReferenced: async (input, signal) => {
        const sourceRequestId = requestIdentity("source-grant");
        const sourceToken = input.droppedFile ? await stageDroppedSource(input.droppedFile) : undefined;
        const selection = await fetch(sourceSelectionPath, {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ requestId: sourceRequestId, ...(sourceToken ? { sourceToken } : {}) }),
          signal,
        });
        if (selection.status === 204) return undefined;
        const selectionData = await readJSON(selection);
        if (!selection.ok) throw await responseError("select source", selection.status, selectionData);
        const sourceResult = asRecord(selectionData);
        const sourceGrant = normalizeSourceGrant(sourceResult.grant);
        if (typeof sourceResult.replayed !== "boolean") throw new Error("source selection receipt is invalid");

        const response = await registerAsset(
          input.projectId,
          {
            requestId: requestIdentity("asset-register"),
            sourceGrantId: sourceGrant.id,
            importMode: "referenced",
            expectedProjectRevision: input.expectedProjectRevision,
          },
          { signal },
        );
        if (response.status !== 200) throw await responseError("register Asset", response.status, response.data);
        const receipt = asRecord(response.data);
        const detail = asRecord(receipt.asset);
        const transaction = asRecord(receipt.transaction);
        if (typeof receipt.replayed !== "boolean") throw new Error("Asset registration receipt is invalid");
        return {
          sourceGrant,
          asset: normalizeAssetDetail(detail),
          transactionId: durableID(transaction.id),
          committedProjectRevision: revisionString(transaction.committedProjectRevision),
          activityCursor: cursorString(receipt.activityCursor),
          replayed: receipt.replayed,
        };
      },
      selectTranscriptDefault: async (projectId, assetId, input, signal) => {
        const artifactId = durableID(input.artifactId);
        const expectedDefaultArtifactId = durableID(input.expectedDefaultArtifactId);
        const response = await selectDefaultTranscript(
          projectId,
          assetId,
          { artifactId, expectedDefaultArtifactId },
          { signal },
        );
        if (response.status !== 200) {
          throw await responseError("select default transcript", response.status, response.data);
        }
        const result = normalizeTranscriptDefaultSelection(
          response.data,
          assetId,
          artifactId,
          expectedDefaultArtifactId,
        );
        projectCursors.set(projectId, result.activityCursor);
        ensureProjectWatch(projectId);
        return result;
      },
    },
    viewer: {
      prepareSourcePreview: async (projectId, assetId, input, signal) => {
        const selection = normalizeSourcePreviewSelection(input);
        const response = await createMediaLease(
          projectId,
          assetId,
          { purpose: "source-preview", ...selection },
          { signal },
        );
        if (response.status !== 200) {
          throw await responseError("prepare source preview", response.status, response.data);
        }
        return normalizeSourcePreviewPreparation(response.data, projectId, assetId, selection);
      },
      resolveSourcePosition: async (projectId, assetId, selectionInput, input, signal) => {
        const selection = normalizeSourcePreviewSelection(selectionInput);
        const resourceId = durableID(input.resourceId);
        if (input.operation !== "settle" && input.operation !== "previous" && input.operation !== "next") {
          throw new Error("source position operation is invalid");
        }
        const target = normalizeRational(input.target);
        const response = await resolveSourcePreviewPosition(
          projectId,
          assetId,
          { resourceId, operation: input.operation, target },
          { signal },
        );
        if (response.status !== 200) {
          throw await responseError("resolve source position", response.status, response.data);
        }
        return normalizeSourcePositionResult(response.data, projectId, assetId, selection, {
          resourceId,
          operation: input.operation,
          target,
        });
      },
      prepareSequencePreview: (projectId, sequenceId, input, signal) =>
        requestSequencePreview("prepare", projectId, sequenceId, input, signal),
      continueSequencePreview: (projectId, sequenceId, input, signal) =>
        requestSequencePreview("continue", projectId, sequenceId, input, signal),
      retrySequencePreview: (projectId, sequenceId, input, signal) =>
        requestSequencePreview("retry", projectId, sequenceId, input, signal),
    },
    notifyProjectChanged,
    close: () => {
      for (const close of projectWatches.values()) close();
      projectWatches.clear();
      projectListeners.clear();
      projectCursors.clear();
    },
  };
}

function normalizeSourcePreviewSelection(input: SourcePreviewSelectionInput): SourcePreviewSelectionInput {
  const selection: SourcePreviewSelectionInput = {
    assetRevision: revisionString(input.assetRevision),
    fingerprint: digestString(input.fingerprint),
    ...(input.videoStreamId === undefined ? {} : { videoStreamId: durableID(input.videoStreamId) }),
    ...(input.audioStreamId === undefined ? {} : { audioStreamId: durableID(input.audioStreamId) }),
  };
  if (selection.videoStreamId === undefined && selection.audioStreamId === undefined) {
    throw new Error("source preview requires an explicit SourceStream selection");
  }
  if (selection.videoStreamId !== undefined && selection.videoStreamId === selection.audioStreamId) {
    throw new Error("source preview stream selection is invalid");
  }
  return selection;
}

async function stageDroppedSource(file: File): Promise<string> {
  if (typeof window === "undefined" || !window.openCutPlatform) {
    throw new Error("Dropped local source staging is unavailable");
  }
  const token = await window.openCutPlatform.stageDroppedSource(file);
  if (!droppedSourceTokenPattern.test(token)) throw new Error("Dropped local source authority is invalid");
  return token;
}

function requestIdentity(kind: string): string {
  return `ui:${kind}:${crypto.randomUUID()}`;
}
