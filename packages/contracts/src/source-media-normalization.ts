import { type DurableID, digestString, durableID, revisionString, uint64String } from "./exact.js";
import type {
  SourcePositionInput,
  SourcePositionResult,
  SourcePreviewLease,
  SourcePreviewPreparation,
  SourcePreviewSelectionInput,
  SourcePreviewTrackTiming,
} from "./media.js";
import { normalizeJob, normalizeMediaDiagnostics, normalizeMediaPreparationStage } from "./media-normalization.js";
import { asRecord, normalizeRational, timestamp } from "./media-validation.js";

export function normalizeSourcePreviewPreparation(
  value: unknown,
  expectedProjectId: DurableID,
  expectedAssetId: DurableID,
  expectedSelection: SourcePreviewSelectionInput,
): SourcePreviewPreparation {
  const preparation = asRecord(value);
  if (
    preparation.purpose !== "source-preview" ||
    (preparation.status !== "ready" && preparation.status !== "preparing" && preparation.status !== "failed")
  ) {
    throw new Error("source preview preparation is invalid");
  }
  const projectId = durableID(preparation.projectId);
  const assetId = durableID(preparation.assetId);
  const assetRevision = revisionString(preparation.assetRevision);
  const fingerprint = digestString(preparation.fingerprint);
  const videoStreamId = optionalDurableID(preparation.videoStreamId);
  const audioStreamId = optionalDurableID(preparation.audioStreamId);
  if (
    projectId !== expectedProjectId ||
    assetId !== expectedAssetId ||
    assetRevision !== expectedSelection.assetRevision ||
    fingerprint !== expectedSelection.fingerprint ||
    videoStreamId !== expectedSelection.videoStreamId ||
    audioStreamId !== expectedSelection.audioStreamId
  ) {
    throw new Error("source preview preparation does not match its pinned selection");
  }
  const job = normalizeJob(preparation.job);
  const stage = normalizeMediaPreparationStage(preparation.stage);
  const diagnostics = normalizeMediaDiagnostics(preparation.diagnostics);
  if (job.kind !== "proxy") throw new Error("source preview does not reference a proxy job");
  if (
    (preparation.status === "ready" && (job.state !== "succeeded" || stage !== undefined)) ||
    (preparation.status === "preparing" &&
      stage === "proxy" &&
      job.state !== "blocked" &&
      job.state !== "queued" &&
      job.state !== "running") ||
    (preparation.status === "preparing" && stage === "integrity" && job.state !== "succeeded") ||
    (preparation.status === "preparing" && stage !== "proxy" && stage !== "integrity") ||
    (preparation.status === "failed" && (stage !== "proxy" || (job.state !== "failed" && job.state !== "cancelled")))
  ) {
    throw new Error("source preview status does not match its proxy job");
  }
  if (preparation.status !== "ready") {
    if (preparation.lease !== undefined) throw new Error("non-ready source preview included a lease");
    return {
      status: preparation.status,
      purpose: "source-preview",
      projectId,
      assetId,
      assetRevision,
      fingerprint,
      ...(videoStreamId === undefined ? {} : { videoStreamId }),
      ...(audioStreamId === undefined ? {} : { audioStreamId }),
      job,
      stage,
      diagnostics,
    };
  }
  const lease = normalizeSourcePreviewLease(preparation.lease, expectedProjectId, expectedAssetId, expectedSelection);
  if (!job.resultArtifactId || job.resultArtifactId !== lease.artifactId) {
    throw new Error("source preview lease does not match its proxy job");
  }
  return {
    status: "ready",
    purpose: "source-preview",
    projectId,
    assetId,
    assetRevision,
    fingerprint,
    ...(videoStreamId === undefined ? {} : { videoStreamId }),
    ...(audioStreamId === undefined ? {} : { audioStreamId }),
    job,
    diagnostics,
    lease,
  };
}

export function normalizeSourcePositionResult(
  value: unknown,
  expectedProjectId: DurableID,
  expectedAssetId: DurableID,
  expectedSelection: SourcePreviewSelectionInput,
  expectedInput: SourcePositionInput,
): SourcePositionResult {
  const result = asRecord(value);
  const resourceId = durableID(result.resourceId);
  const projectId = durableID(result.projectId);
  const assetId = durableID(result.assetId);
  const assetRevision = revisionString(result.assetRevision);
  const fingerprint = digestString(result.fingerprint);
  const videoStreamId = optionalDurableID(result.videoStreamId);
  const audioStreamId = optionalDurableID(result.audioStreamId);
  const requestedTime = normalizeRational(result.requestedTime);
  if (
    resourceId !== expectedInput.resourceId ||
    projectId !== expectedProjectId ||
    assetId !== expectedAssetId ||
    assetRevision !== expectedSelection.assetRevision ||
    fingerprint !== expectedSelection.fingerprint ||
    videoStreamId !== expectedSelection.videoStreamId ||
    audioStreamId !== expectedSelection.audioStreamId ||
    result.operation !== expectedInput.operation ||
    !sameRational(requestedTime, expectedInput.target) ||
    (result.boundary !== "video-presentation" &&
      result.boundary !== "audio-sample" &&
      result.boundary !== "coverage-end") ||
    typeof result.atStart !== "boolean" ||
    typeof result.atEnd !== "boolean" ||
    (videoStreamId !== undefined && result.boundary === "audio-sample") ||
    (videoStreamId === undefined && result.boundary === "video-presentation")
  ) {
    throw new Error("source position result does not match its pinned lease request");
  }
  const sourceTime = normalizeRational(result.sourceTime);
  const proxyTime = normalizeRational(result.proxyTime);
  if (BigInt(proxyTime.value) < 0n) throw new Error("source position proxy time is invalid");
  return {
    resourceId,
    projectId,
    assetId,
    assetRevision,
    fingerprint,
    ...(videoStreamId === undefined ? {} : { videoStreamId }),
    ...(audioStreamId === undefined ? {} : { audioStreamId }),
    operation: expectedInput.operation,
    requestedTime,
    sourceTime,
    proxyTime,
    boundary: result.boundary,
    atStart: result.atStart,
    atEnd: result.atEnd,
  };
}

function normalizeSourcePreviewLease(
  value: unknown,
  expectedProjectId: DurableID,
  expectedAssetId: DurableID,
  expectedSelection: SourcePreviewSelectionInput,
): SourcePreviewLease {
  const lease = asRecord(value);
  if (
    lease.schema !== "open-cut/media-lease/v1" ||
    lease.purpose !== "source-preview" ||
    (lease.mimeType !== "video/webm" && lease.mimeType !== "audio/webm") ||
    typeof lease.etag !== "string" ||
    !/^"sha256-[0-9a-f]{64}"$/.test(lease.etag) ||
    typeof lease.sameOriginUrl !== "string" ||
    !/^\/api\/v1\/media\/content\/oc_media_[A-Za-z0-9_-]{43}$/.test(lease.sameOriginUrl) ||
    typeof lease.expiresAt !== "string"
  ) {
    throw new Error("source preview lease is invalid");
  }
  const projectId = durableID(lease.projectId);
  const assetId = durableID(lease.assetId);
  const assetRevision = revisionString(lease.assetRevision);
  const fingerprint = digestString(lease.fingerprint);
  if (
    projectId !== expectedProjectId ||
    assetId !== expectedAssetId ||
    assetRevision !== expectedSelection.assetRevision ||
    fingerprint !== expectedSelection.fingerprint
  ) {
    throw new Error("source preview lease does not match its pinned selection");
  }
  const video = normalizeSourcePreviewTrackTiming(lease.video, expectedSelection.videoStreamId);
  const audio = normalizeSourcePreviewTrackTiming(lease.audio, expectedSelection.audioStreamId);
  if (
    (video !== undefined && lease.mimeType !== "video/webm") ||
    (video === undefined && lease.mimeType !== "audio/webm")
  ) {
    throw new Error("source preview lease MIME does not match its selected streams");
  }
  return {
    schema: "open-cut/media-lease/v1",
    resourceId: durableID(lease.resourceId),
    purpose: "source-preview",
    projectId,
    assetId,
    assetRevision,
    fingerprint,
    artifactId: durableID(lease.artifactId),
    artifactDigest: digestString(lease.artifactDigest),
    mimeType: lease.mimeType,
    byteLength: uint64String(lease.byteLength),
    etag: lease.etag,
    sameOriginUrl: lease.sameOriginUrl,
    expiresAt: timestamp(lease.expiresAt),
    sourceEpoch: normalizeRational(lease.sourceEpoch),
    ...(video === undefined ? {} : { video }),
    ...(audio === undefined ? {} : { audio }),
  };
}

function normalizeSourcePreviewTrackTiming(
  value: unknown,
  expectedStreamId: DurableID | undefined,
): SourcePreviewTrackTiming | undefined {
  if (expectedStreamId === undefined) {
    if (value !== undefined) throw new Error("source preview lease contains an unselected track");
    return undefined;
  }
  const timing = asRecord(value);
  const sourceStreamId = durableID(timing.sourceStreamId);
  const coverageStart = normalizeRational(timing.coverageStart);
  const coverageDuration =
    timing.coverageDuration === undefined ? undefined : normalizeRational(timing.coverageDuration);
  const sourceStartTime = normalizeRational(timing.sourceStartTime);
  const proxyStartTime = normalizeRational(timing.proxyStartTime);
  const sourceTimeBase = normalizeRational(timing.sourceTimeBase);
  const proxyTimeBase = normalizeRational(timing.proxyTimeBase);
  if (
    sourceStreamId !== expectedStreamId ||
    (coverageDuration !== undefined && BigInt(coverageDuration.value) < 0n) ||
    BigInt(proxyStartTime.value) < 0n ||
    BigInt(sourceTimeBase.value) <= 0n ||
    BigInt(proxyTimeBase.value) <= 0n
  ) {
    throw new Error("source preview track timing is invalid");
  }
  return {
    sourceStreamId,
    coverageStart,
    ...(coverageDuration === undefined ? {} : { coverageDuration }),
    sourceStartTime,
    proxyStartTime,
    sourceTimeBase,
    proxyTimeBase,
  };
}

function optionalDurableID(value: unknown): DurableID | undefined {
  return value === undefined ? undefined : durableID(value);
}

function sameRational(left: { value: string; scale: number }, right: { value: string; scale: number }): boolean {
  return left.value === right.value && left.scale === right.scale;
}
