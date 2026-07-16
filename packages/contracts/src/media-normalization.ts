import {
  cursorString,
  type DurableID,
  digestString,
  durableID,
  int64String,
  type RevisionString,
  revisionString,
  uint64String,
} from "./exact.js";
import type {
  Asset,
  AssetPage,
  AudioStreamFacts,
  MediaArtifact,
  MediaDiagnostic,
  MediaFacts,
  MediaJob,
  MediaJobPrerequisite,
  MediaPreparationStage,
  SequencePreviewContinuation,
  SequencePreviewJob,
  SequencePreviewLease,
  SequencePreviewMediaFacts,
  SequencePreviewPreparation,
  SourceGrant,
  SourceStream,
  SourceStreamDescriptor,
  VideoStreamFacts,
} from "./media.js";
import {
  asRecord,
  isBoundedInteger,
  isMediaType,
  isString,
  normalizeRational,
  normalizeTextList,
  optionalInteger,
  optionalText,
  timestamp,
} from "./media-validation.js";

export function normalizeSequencePreviewPreparation(
  value: unknown,
  expectedProjectId: DurableID,
  expectedSequenceId: DurableID,
  expectedRevision: RevisionString,
): SequencePreviewPreparation {
  const preparation = asRecord(value);
  if (
    preparation.purpose !== "sequence-preview" ||
    (preparation.status !== "empty" &&
      preparation.status !== "ready" &&
      preparation.status !== "preparing" &&
      preparation.status !== "failed")
  ) {
    throw new Error("sequence preview preparation is invalid");
  }
  const projectId = durableID(preparation.projectId);
  const sequenceId = durableID(preparation.sequenceId);
  const sequenceRevision = revisionString(preparation.sequenceRevision);
  if (projectId !== expectedProjectId || sequenceId !== expectedSequenceId || sequenceRevision !== expectedRevision) {
    throw new Error("sequence preview preparation does not match its request");
  }
  const diagnostics = normalizeMediaDiagnostics(preparation.diagnostics);
  if (preparation.status === "empty") {
    if (
      preparation.job !== undefined ||
      preparation.continuation !== undefined ||
      preparation.stage !== undefined ||
      preparation.lease !== undefined
    ) {
      throw new Error("empty sequence preview contains work state");
    }
    return {
      status: "empty",
      purpose: "sequence-preview",
      projectId,
      sequenceId,
      sequenceRevision,
      diagnostics,
    };
  }
  const job = normalizeSequencePreviewJob(preparation.job);
  const continuation = normalizeSequencePreviewContinuation(preparation.continuation, job);
  const stage = normalizeMediaPreparationStage(preparation.stage);
  if (
    (preparation.status === "preparing" &&
      ((stage === "render" && job.state !== "blocked" && job.state !== "queued" && job.state !== "running") ||
        (stage === "integrity" && job.state !== "succeeded") ||
        (stage !== "render" && stage !== "integrity"))) ||
    (preparation.status === "failed" &&
      (stage !== "render" || (job.state !== "failed" && job.state !== "cancelled"))) ||
    (preparation.status === "ready" && (stage !== undefined || job.state !== "succeeded"))
  ) {
    throw new Error("sequence preview status does not match its work job");
  }
  if (preparation.status !== "ready") {
    if (preparation.lease !== undefined) throw new Error("non-ready sequence preview included a lease");
    return {
      status: preparation.status,
      purpose: "sequence-preview",
      projectId,
      sequenceId,
      sequenceRevision,
      job,
      continuation,
      stage: stage as "render" | "integrity",
      diagnostics,
    };
  }
  const lease = normalizeSequencePreviewLease(
    preparation.lease,
    expectedProjectId,
    expectedSequenceId,
    expectedRevision,
  );
  if (
    !job.resultArtifactId ||
    !job.renderPlanDigest ||
    job.resultArtifactId !== lease.artifactId ||
    job.renderPlanDigest !== lease.renderPlanDigest
  ) {
    throw new Error("sequence preview lease does not match its work job");
  }
  return {
    status: "ready",
    purpose: "sequence-preview",
    projectId,
    sequenceId,
    sequenceRevision,
    job,
    continuation,
    diagnostics,
    lease,
  };
}

function normalizeSequencePreviewContinuation(value: unknown, job: SequencePreviewJob): SequencePreviewContinuation {
  const candidate = asRecord(value);
  const continuation: SequencePreviewContinuation = {
    jobId: durableID(candidate.jobId),
    ...(candidate.renderPlanDigest === undefined ? {} : { renderPlanDigest: digestString(candidate.renderPlanDigest) }),
  };
  if (
    continuation.jobId !== job.id ||
    (continuation.renderPlanDigest !== undefined && continuation.renderPlanDigest !== job.renderPlanDigest)
  ) {
    throw new Error("sequence preview continuation does not match its work job");
  }
  return continuation;
}

function normalizeSequencePreviewJob(value: unknown): SequencePreviewJob {
  const job = asRecord(value);
  if (
    job.kind !== "sequence-preview" ||
    (job.state !== "blocked" &&
      job.state !== "queued" &&
      job.state !== "running" &&
      job.state !== "succeeded" &&
      job.state !== "failed" &&
      job.state !== "cancelled") ||
    !isBoundedInteger(job.progressBasisPoints, 0, 10_000) ||
    (job.terminalErrorCode !== undefined && !isString(job.terminalErrorCode, 1, 256))
  ) {
    throw new Error("sequence preview work job is invalid");
  }
  return {
    id: durableID(job.id),
    kind: "sequence-preview",
    state: job.state,
    progressBasisPoints: job.progressBasisPoints,
    ...(isString(job.terminalErrorCode, 1, 256) ? { terminalErrorCode: job.terminalErrorCode } : {}),
    ...(job.renderPlanDigest === undefined ? {} : { renderPlanDigest: digestString(job.renderPlanDigest) }),
    ...(job.resultArtifactId === undefined ? {} : { resultArtifactId: durableID(job.resultArtifactId) }),
    createdAt: timestamp(job.createdAt),
    updatedAt: timestamp(job.updatedAt),
  };
}

function normalizeSequencePreviewLease(
  value: unknown,
  expectedProjectId: DurableID,
  expectedSequenceId: DurableID,
  expectedRevision: RevisionString,
): SequencePreviewLease {
  const lease = asRecord(value);
  if (
    lease.schema !== "open-cut/media-lease/v1" ||
    lease.purpose !== "sequence-preview" ||
    lease.mimeType !== "video/webm" ||
    typeof lease.etag !== "string" ||
    !/^"sha256-[0-9a-f]{64}"$/.test(lease.etag) ||
    typeof lease.sameOriginUrl !== "string" ||
    !/^\/api\/v1\/media\/content\/oc_sequence_[A-Za-z0-9_-]{43}$/.test(lease.sameOriginUrl)
  ) {
    throw new Error("sequence preview lease is invalid");
  }
  const projectId = durableID(lease.projectId);
  const sequenceId = durableID(lease.sequenceId);
  const sequenceRevision = revisionString(lease.sequenceRevision);
  if (projectId !== expectedProjectId || sequenceId !== expectedSequenceId || sequenceRevision !== expectedRevision) {
    throw new Error("sequence preview lease does not match its request");
  }
  return {
    schema: "open-cut/media-lease/v1",
    resourceId: durableID(lease.resourceId),
    purpose: "sequence-preview",
    projectId,
    sequenceId,
    sequenceRevision,
    renderPlanDigest: digestString(lease.renderPlanDigest),
    artifactId: durableID(lease.artifactId),
    artifactDigest: digestString(lease.artifactDigest),
    facts: normalizeSequencePreviewFacts(lease.facts),
    mimeType: "video/webm",
    byteLength: uint64String(lease.byteLength),
    etag: lease.etag,
    sameOriginUrl: lease.sameOriginUrl,
    expiresAt: timestamp(lease.expiresAt),
  };
}

function normalizeSequencePreviewFacts(value: unknown): SequencePreviewMediaFacts {
  const facts = asRecord(value);
  if (
    !isBoundedInteger(facts.canvasWidth, 1, 16_384) ||
    !isBoundedInteger(facts.canvasHeight, 1, 16_384) ||
    facts.audioSampleRate !== 48_000 ||
    facts.videoCodec !== "vp9" ||
    facts.audioCodec !== "opus" ||
    facts.pixelFormat !== "yuv420p" ||
    facts.channelLayout !== "stereo"
  ) {
    throw new Error("sequence preview media facts are invalid");
  }
  return {
    semanticDuration: normalizeRational(facts.semanticDuration),
    presentationDuration: normalizeRational(facts.presentationDuration),
    canvasWidth: facts.canvasWidth,
    canvasHeight: facts.canvasHeight,
    frameRate: normalizeRational(facts.frameRate),
    videoFrameCount: uint64String(facts.videoFrameCount),
    audioSampleRate: 48_000,
    audioSampleCount: uint64String(facts.audioSampleCount),
    videoCodec: "vp9",
    audioCodec: "opus",
    pixelFormat: "yuv420p",
    channelLayout: "stereo",
  };
}

export function normalizeMediaPreparationStage(value: unknown): MediaPreparationStage | undefined {
  if (value === undefined) return undefined;
  if (value !== "proxy" && value !== "integrity" && value !== "render") {
    throw new Error("media preparation stage is invalid");
  }
  return value;
}

export function normalizeMediaDiagnostics(value: unknown): readonly MediaDiagnostic[] {
  if (!Array.isArray(value) || value.length > 32) throw new Error("media diagnostics are invalid");
  return value.map((item) => {
    const diagnostic = asRecord(item);
    if (
      (diagnostic.code !== "source-proxy-integrity-rejected" &&
        diagnostic.code !== "source-proxy-job-failed" &&
        diagnostic.code !== "source-proxy-job-cancelled" &&
        diagnostic.code !== "sequence-preview-integrity-rejected" &&
        diagnostic.code !== "sequence-preview-job-failed" &&
        diagnostic.code !== "sequence-preview-job-cancelled") ||
      (diagnostic.severity !== "degraded" && diagnostic.severity !== "blocking") ||
      (diagnostic.subjectKind !== "asset" &&
        diagnostic.subjectKind !== "media-job" &&
        diagnostic.subjectKind !== "work-job" &&
        diagnostic.subjectKind !== "artifact") ||
      (diagnostic.recovery !== "automatic-retry" &&
        diagnostic.recovery !== "retry-job" &&
        diagnostic.recovery !== "relink-source" &&
        diagnostic.recovery !== "acquire-resource" &&
        diagnostic.recovery !== "adopt-revision" &&
        diagnostic.recovery !== "update-runtime" &&
        diagnostic.recovery !== "none")
    ) {
      throw new Error("media diagnostic is invalid");
    }
    return {
      code: diagnostic.code,
      severity: diagnostic.severity,
      subjectKind: diagnostic.subjectKind,
      subjectId: durableID(diagnostic.subjectId),
      recovery: diagnostic.recovery,
    };
  });
}

export function normalizeSourceGrant(value: unknown): SourceGrant {
  const grant = asRecord(value);
  if (
    (grant.platform !== "mac" && grant.platform !== "win" && grant.platform !== "linux") ||
    (grant.kind !== "local-path-v1" && grant.kind !== "mac-security-scoped-bookmark-v1") ||
    (grant.state !== "active" && grant.state !== "revoked" && grant.state !== "unavailable") ||
    !isString(grant.displayName, 1, 512)
  ) {
    throw new Error("SourceGrant payload is invalid");
  }
  const observation = asRecord(grant.observation);
  if (!isString(observation.fileIdentity, 1, 512)) throw new Error("source observation is invalid");
  return {
    id: durableID(grant.id),
    platform: grant.platform,
    kind: grant.kind,
    displayName: grant.displayName,
    observation: {
      byteSize: uint64String(observation.byteSize),
      modifiedUnixNs: int64String(observation.modifiedUnixNs),
      fileIdentity: observation.fileIdentity,
    },
    state: grant.state,
    createdAt: timestamp(grant.createdAt),
  };
}

export function normalizeAssetPage(value: unknown): AssetPage {
  const page = asRecord(value);
  if (!Array.isArray(page.assets) || page.assets.length > 100) throw new Error("Asset page is invalid");
  return {
    assets: page.assets.map(normalizeAsset),
    ...(typeof page.nextAfter === "string" ? { nextAfter: page.nextAfter } : {}),
    activityCursor: cursorString(page.activityCursor),
  };
}

export function normalizeAssetDetail(value: Record<string, unknown>): Asset {
  const state = asRecord(value.asset);
  return normalizeAsset({
    ...state,
    availability: value.availability,
    fingerprint: value.fingerprint,
    facts: value.facts,
    artifacts: value.artifacts,
    jobs: value.jobs,
  });
}

export function normalizeAsset(value: unknown): Asset {
  const asset = asRecord(value);
  if (
    !isString(asset.displayName, 1, 512) ||
    (asset.importMode !== "referenced" && asset.importMode !== "managed") ||
    typeof asset.tombstoned !== "boolean" ||
    (asset.availability !== "identifying" &&
      asset.availability !== "online" &&
      asset.availability !== "changed" &&
      asset.availability !== "missing" &&
      asset.availability !== "managed" &&
      asset.availability !== "unreadable") ||
    !Array.isArray(asset.artifacts) ||
    asset.artifacts.length > 32 ||
    !Array.isArray(asset.jobs) ||
    asset.jobs.length > 32
  ) {
    throw new Error("Asset payload is invalid");
  }
  return {
    id: durableID(asset.id),
    revision: revisionString(asset.revision),
    projectId: durableID(asset.projectId),
    displayName: asset.displayName,
    importMode: asset.importMode,
    ...(asset.acceptedFingerprint === undefined
      ? {}
      : { acceptedFingerprint: digestString(asset.acceptedFingerprint) }),
    tombstoned: asset.tombstoned,
    availability: asset.availability,
    ...(asset.fingerprint === undefined ? {} : { fingerprint: digestString(asset.fingerprint) }),
    ...(asset.facts === undefined ? {} : { facts: normalizeFacts(asset.facts) }),
    artifacts: asset.artifacts.map(normalizeArtifact),
    jobs: asset.jobs.map(normalizeJob),
  };
}

function normalizeFacts(value: unknown): MediaFacts {
  const facts = asRecord(value);
  if (
    !isString(facts.container, 1, 128) ||
    !Array.isArray(facts.containerAliases) ||
    facts.containerAliases.length > 32 ||
    !Array.isArray(facts.streams) ||
    facts.streams.length === 0 ||
    facts.streams.length > 64
  ) {
    throw new Error("media facts are invalid");
  }
  return {
    container: facts.container,
    containerAliases: normalizeTextList(facts.containerAliases, 128, false),
    ...(facts.startTime === undefined ? {} : { startTime: normalizeRational(facts.startTime) }),
    ...(facts.duration === undefined ? {} : { duration: normalizeRational(facts.duration) }),
    ...(facts.bitRate === undefined ? {} : { bitRate: uint64String(facts.bitRate) }),
    streams: facts.streams.map(normalizeStream),
  };
}

function normalizeStream(value: unknown): SourceStream {
  const stream = asRecord(value);
  return {
    id: durableID(stream.id),
    descriptor: normalizeStreamDescriptor(stream.descriptor),
  };
}

function normalizeStreamDescriptor(value: unknown): SourceStreamDescriptor {
  const descriptor = asRecord(value);
  if (
    !isBoundedInteger(descriptor.index, 0, 4_294_967_295) ||
    !isMediaType(descriptor.mediaType) ||
    !isString(descriptor.codec, 1, 128) ||
    !Array.isArray(descriptor.dispositions) ||
    descriptor.dispositions.length > 32
  ) {
    throw new Error("source stream descriptor is invalid");
  }
  const video = descriptor.video === undefined ? undefined : normalizeVideoFacts(descriptor.video);
  const audio = descriptor.audio === undefined ? undefined : normalizeAudioFacts(descriptor.audio);
  if (
    (descriptor.mediaType === "video" && (!video || audio)) ||
    (descriptor.mediaType === "audio" && (!audio || video)) ||
    (descriptor.mediaType !== "video" && descriptor.mediaType !== "audio" && (video || audio))
  ) {
    throw new Error("source stream media facts do not match its type");
  }
  return {
    index: descriptor.index,
    mediaType: descriptor.mediaType,
    codec: descriptor.codec,
    ...optionalText(descriptor, "codecProfile", 128),
    ...optionalText(descriptor, "codecTag", 64),
    timeBase: normalizeRational(descriptor.timeBase),
    ...(descriptor.startTime === undefined ? {} : { startTime: normalizeRational(descriptor.startTime) }),
    ...(descriptor.duration === undefined ? {} : { duration: normalizeRational(descriptor.duration) }),
    ...optionalText(descriptor, "language", 64),
    dispositions: normalizeTextList(descriptor.dispositions, 64, true),
    ...(video ? { video } : {}),
    ...(audio ? { audio } : {}),
  };
}

function normalizeVideoFacts(value: unknown): VideoStreamFacts {
  const video = asRecord(value);
  if (
    !isBoundedInteger(video.width, 1, 32_768) ||
    !isBoundedInteger(video.height, 1, 32_768) ||
    (video.rotation !== 0 && video.rotation !== 90 && video.rotation !== 180 && video.rotation !== 270)
  ) {
    throw new Error("video stream facts are invalid");
  }
  return {
    width: video.width,
    height: video.height,
    ...optionalInteger(video, "codedWidth", 0, 32_768),
    ...optionalInteger(video, "codedHeight", 0, 32_768),
    ...(video.pixelAspect === undefined ? {} : { pixelAspect: normalizeRational(video.pixelAspect) }),
    ...(video.averageRate === undefined ? {} : { averageRate: normalizeRational(video.averageRate) }),
    ...(video.nominalRate === undefined ? {} : { nominalRate: normalizeRational(video.nominalRate) }),
    rotation: video.rotation,
    ...optionalText(video, "pixelFormat", 64),
    ...optionalText(video, "colorRange", 64),
    ...optionalText(video, "colorSpace", 64),
    ...optionalText(video, "colorTransfer", 64),
    ...optionalText(video, "colorPrimaries", 64),
  };
}

function normalizeAudioFacts(value: unknown): AudioStreamFacts {
  const audio = asRecord(value);
  if (!isBoundedInteger(audio.sampleRate, 1, 768_000) || !isBoundedInteger(audio.channels, 1, 64)) {
    throw new Error("audio stream facts are invalid");
  }
  return {
    ...optionalText(audio, "sampleFormat", 64),
    sampleRate: audio.sampleRate,
    channels: audio.channels,
    ...optionalText(audio, "channelLayout", 128),
  };
}

function normalizeArtifact(value: unknown): MediaArtifact {
  const artifact = asRecord(value);
  if (
    artifact.kind !== "media-facts" &&
    artifact.kind !== "frame-sample-set" &&
    artifact.kind !== "proxy" &&
    artifact.kind !== "waveform" &&
    artifact.kind !== "transcript"
  ) {
    throw new Error("media artifact kind is invalid");
  }
  if ((artifact.state !== "ready" && artifact.state !== "evicted") || typeof artifact.producerVersion !== "string") {
    throw new Error("media artifact is invalid");
  }
  return {
    id: durableID(artifact.id),
    kind: artifact.kind,
    producerVersion: artifact.producerVersion,
    inputFingerprint: digestString(artifact.inputFingerprint),
    state: artifact.state,
    byteSize: uint64String(artifact.byteSize),
    contentDigest: digestString(artifact.contentDigest),
    createdAt: timestamp(artifact.createdAt),
  };
}

export function normalizeJob(value: unknown): MediaJob {
  const job = asRecord(value);
  if (
    job.kind !== "identify" &&
    job.kind !== "probe" &&
    job.kind !== "frame-sample-set" &&
    job.kind !== "proxy" &&
    job.kind !== "waveform" &&
    job.kind !== "transcript"
  ) {
    throw new Error("media job kind is invalid");
  }
  if (
    job.state !== "blocked" &&
    job.state !== "queued" &&
    job.state !== "running" &&
    job.state !== "succeeded" &&
    job.state !== "failed" &&
    job.state !== "cancelled"
  ) {
    throw new Error("media job state is invalid");
  }
  if (
    !isBoundedInteger(job.progressBasisPoints, 0, 10_000) ||
    !Array.isArray(job.prerequisites) ||
    job.prerequisites.length > 8 ||
    (job.state === "blocked") !== job.prerequisites.length > 0 ||
    (job.state === "failed") !== isString(job.terminalErrorCode, 1, 256)
  ) {
    throw new Error("media job is invalid");
  }
  const prerequisites = job.prerequisites.map(normalizeJobPrerequisite);
  if (new Set(prerequisites.map(prerequisiteKey)).size !== prerequisites.length) {
    throw new Error("media job repeats a prerequisite");
  }
  return {
    id: durableID(job.id),
    kind: job.kind,
    state: job.state,
    progressBasisPoints: job.progressBasisPoints,
    prerequisites,
    ...(isString(job.terminalErrorCode, 1, 256) ? { terminalErrorCode: job.terminalErrorCode } : {}),
    ...(job.resultArtifactId === undefined ? {} : { resultArtifactId: durableID(job.resultArtifactId) }),
    createdAt: timestamp(job.createdAt),
    updatedAt: timestamp(job.updatedAt),
  };
}

function normalizeJobPrerequisite(value: unknown): MediaJobPrerequisite {
  const prerequisite = asRecord(value);
  if (
    (prerequisite.kind === "fingerprint-required" || prerequisite.kind === "facts-required") &&
    prerequisite.resourceId === undefined &&
    prerequisite.capability === undefined
  ) {
    return { kind: prerequisite.kind, jobId: durableID(prerequisite.jobId) };
  }
  if (
    prerequisite.kind === "model-required" &&
    prerequisite.jobId === undefined &&
    prerequisite.capability === undefined &&
    isString(prerequisite.resourceId, 1, 256)
  ) {
    return { kind: prerequisite.kind, resourceId: prerequisite.resourceId };
  }
  if (
    prerequisite.kind === "executor-required" &&
    prerequisite.jobId === undefined &&
    prerequisite.resourceId === undefined &&
    isString(prerequisite.capability, 1, 256)
  ) {
    return { kind: prerequisite.kind, capability: prerequisite.capability };
  }
  throw new Error("media job prerequisite is invalid");
}

function prerequisiteKey(prerequisite: MediaJobPrerequisite): string {
  if ("jobId" in prerequisite) return `${prerequisite.kind}/job/${prerequisite.jobId}`;
  if ("resourceId" in prerequisite) return `${prerequisite.kind}/resource/${prerequisite.resourceId}`;
  return `${prerequisite.kind}/capability/${prerequisite.capability}`;
}
