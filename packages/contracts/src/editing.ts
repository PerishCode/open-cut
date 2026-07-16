import { showNarrativeSubtree, showSequenceWindow } from "@open-cut/openapi/editing";
import { type CreatorCaptionPort, createCreatorCaptionPort } from "./caption.js";
import { type CaptionDerivationPolicy, normalizeCaptionDerivationPolicy } from "./caption-policy.js";
import { type CreatorClipPlacementPort, createCreatorClipPlacementPort } from "./clip-placement.js";
import { createEditWritePort, type EditWritePort } from "./creator-editing.js";
import { type CreatorHistoryPort, createCreatorHistoryPort } from "./creator-history.js";
import { asRecord, canonicalLanguage, normalizeTimeRange, type TimeRange, timeRangeKey } from "./editing-exact.js";
import {
  type CursorString,
  cursorString,
  type DigestString,
  type DurableID,
  digestString,
  durableID,
  type RevisionString,
  revisionString,
} from "./exact.js";
import { type CreatorManualCaptionPort, createCreatorManualCaptionPort } from "./manual-caption.js";
import { type CreatorRoughCutPort, createCreatorRoughCutPort } from "./rough-cut.js";
import { type CreatorTimelinePort, createCreatorTimelinePort } from "./timeline.js";

export type { CaptionDerivationPolicy } from "./caption-policy.js";
export type { TimeRange } from "./editing-exact.js";

export type AuthoredText = Readonly<{
  id: DurableID;
  revision: RevisionString;
  documentId: DurableID;
  parentId: DurableID;
  afterNodeId?: DurableID;
  purpose: "spoken" | "on-screen";
  language: string;
  text: string;
  tombstoned: boolean;
}>;

export type NarrativeSection = Readonly<{
  id: DurableID;
  revision: RevisionString;
  documentId: DurableID;
  parentId?: DurableID;
  afterNodeId?: DurableID;
  title: string;
  language: string;
  tombstoned: boolean;
}>;

export type VisualIntent = Readonly<{
  id: DurableID;
  revision: RevisionString;
  documentId: DurableID;
  parentId: DurableID;
  afterNodeId?: DurableID;
  purpose: "b-roll" | "composition" | "replacement";
  language: string;
  description: string;
  tombstoned: boolean;
}>;

export type NarrativeNote = Readonly<{
  id: DurableID;
  revision: RevisionString;
  documentId: DurableID;
  parentId: DurableID;
  afterNodeId?: DurableID;
  language: string;
  text: string;
  tombstoned: boolean;
}>;

export type TranscriptCorrectionRevision = Readonly<{
  id: DurableID;
  revision: RevisionString;
}>;

export type SourceExcerptEvidence = Readonly<{
  artifactId: DurableID;
  sourceStreamId: DurableID;
  segmentIds: readonly DurableID[];
  correctionRevisions: readonly TranscriptCorrectionRevision[];
}>;

export type SourceExcerpt = Readonly<{
  id: DurableID;
  revision: RevisionString;
  documentId: DurableID;
  parentId: DurableID;
  afterNodeId?: DurableID;
  assetId: DurableID;
  acceptedFingerprint: DigestString;
  sourceRange: TimeRange;
  language: string;
  effectiveText: string;
  evidence: SourceExcerptEvidence;
  tombstoned: boolean;
}>;

export type NarrativeNode =
  | Readonly<{ kind: "section"; section: NarrativeSection }>
  | Readonly<{ kind: "authored-text"; authoredText: AuthoredText }>
  | Readonly<{ kind: "source-excerpt"; sourceExcerpt: SourceExcerpt; evidenceStatus: "exact" | "stale" }>
  | Readonly<{ kind: "visual-intent"; visualIntent: VisualIntent }>
  | Readonly<{ kind: "note"; note: NarrativeNote }>;

export type NarrativeSectionSummary = Readonly<{
  id: DurableID;
  revision: RevisionString;
  title: string;
  language: string;
}>;

export type NarrativeSubtree = Readonly<{
  documentId: DurableID;
  documentRevision: RevisionString;
  parent: NarrativeSectionSummary;
  nodes: readonly NarrativeNode[];
  nextAfter?: string;
  activityCursor: CursorString;
}>;

export type Caption = Readonly<{
  id: DurableID;
  revision: RevisionString;
  sequenceId: DurableID;
  trackId: DurableID;
  range: TimeRange;
  language: string;
  text: string;
  provenance: CaptionProvenance;
  provenanceStatus?: CaptionProvenanceStatus;
  tombstoned: boolean;
}>;

export type CaptionDerivationProvenance = Readonly<{
  sourceExcerptId: DurableID;
  sourceExcerptRevision: RevisionString;
  assetId: DurableID;
  acceptedFingerprint: DigestString;
  transcriptArtifactId: DurableID;
  sourceStreamId: DurableID;
  segmentIds: readonly DurableID[];
  correctionRevisions: readonly TranscriptCorrectionRevision[];
  clipId: DurableID;
  clipRevision: RevisionString;
  clipSourceRange: TimeRange;
  clipTimelineRange: TimeRange;
  evidenceSourceRange: TimeRange;
  policy: CaptionDerivationPolicy;
  derivedRange: TimeRange;
  derivedLanguage: string;
  derivedText: string;
}>;

export type CaptionProvenance =
  | Readonly<{ kind: "manual" }>
  | Readonly<{ kind: "transcript-derivation"; derivation: CaptionDerivationProvenance }>;

export type CaptionProvenanceStatus = Readonly<{
  content: "exact" | "modified";
  evidence: "exact" | "stale";
}>;

export type Clip = Readonly<{
  id: DurableID;
  revision: RevisionString;
  sequenceId: DurableID;
  trackId: DurableID;
  assetId: DurableID;
  sourceStreamId: DurableID;
  sourceRange: TimeRange;
  timelineRange: TimeRange;
  enabled: boolean;
  linkGroupId?: DurableID;
  tombstoned: boolean;
}>;

export type LinkGroup = Readonly<{
  id: DurableID;
  revision: RevisionString;
  sequenceId: DurableID;
  tombstoned: boolean;
}>;

export type CaptionAlignmentTarget = Readonly<{
  captionId: DurableID;
  captionRevision: RevisionString;
  localRange: TimeRange;
}>;

export type ClipAlignmentTarget = Readonly<{
  clipId: DurableID;
  clipRevision: RevisionString;
  localRange: TimeRange;
}>;

export type TimelineAlignmentTarget = Readonly<{
  sequenceRevision: RevisionString;
  range: TimeRange;
}>;

export type AlignmentTarget =
  | Readonly<{ type: "caption"; caption: CaptionAlignmentTarget }>
  | Readonly<{ type: "clip"; clip: ClipAlignmentTarget }>
  | Readonly<{ type: "timeline"; timeline: TimelineAlignmentTarget }>;

export type Alignment = Readonly<{
  id: DurableID;
  revision: RevisionString;
  narrativeNodeId: DurableID;
  narrativeNodeRevision: RevisionString;
  sequenceId: DurableID;
  targets: readonly AlignmentTarget[];
  status: "exact" | "stale" | "unbound";
}>;

export type SequenceWindow = Readonly<{
  sequenceId: DurableID;
  sequenceRevision: RevisionString;
  range: TimeRange;
  clips: readonly Clip[];
  linkGroups: readonly LinkGroup[];
  captions: readonly Caption[];
  alignments: readonly Alignment[];
  nextAfter?: string;
  activityCursor: CursorString;
}>;

export type NarrativeSubtreeInput = Readonly<{
  projectId: DurableID;
  documentId: DurableID;
  parentId: DurableID;
  after?: string;
  limit?: number;
}>;

export type SequenceWindowInput = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  trackId?: DurableID;
  range: TimeRange;
  after?: string;
  limit?: number;
}>;

export interface EditReadPort {
  narrativeSubtree(input: NarrativeSubtreeInput, signal?: AbortSignal): Promise<NarrativeSubtree>;
  sequenceWindow(input: SequenceWindowInput, signal?: AbortSignal): Promise<SequenceWindow>;
}

export type EditingPorts = Readonly<{
  read: EditReadPort;
  write: EditWritePort;
  captions: CreatorCaptionPort;
  manualCaptions: CreatorManualCaptionPort;
  clipPlacement: CreatorClipPlacementPort;
  roughCut: CreatorRoughCutPort;
  timeline: CreatorTimelinePort;
  history: CreatorHistoryPort;
}>;

export function createEditingPorts(): EditingPorts {
  return {
    read: {
      narrativeSubtree: async (input, signal) => {
        const response = await showNarrativeSubtree(
          input.projectId,
          input.documentId,
          {
            parentId: input.parentId,
            ...(input.after === undefined ? {} : { after: input.after }),
            ...(input.limit === undefined ? {} : { limit: input.limit }),
          },
          { signal },
        );
        if (response.status !== 200) throw new Error(`show Narrative returned ${response.status}`);
        return normalizeNarrativeSubtree(response.data);
      },
      sequenceWindow: async (input, signal) => {
        const response = await showSequenceWindow(
          input.projectId,
          input.sequenceId,
          {
            startValue: input.range.start.value,
            startScale: input.range.start.scale,
            durationValue: input.range.duration.value,
            durationScale: input.range.duration.scale,
            ...(input.trackId === undefined ? {} : { trackId: input.trackId }),
            ...(input.after === undefined ? {} : { after: input.after }),
            ...(input.limit === undefined ? {} : { limit: input.limit }),
          },
          { signal },
        );
        if (response.status !== 200) throw new Error(`show Sequence returned ${response.status}`);
        return normalizeSequenceWindow(response.data);
      },
    },
    write: createEditWritePort(),
    captions: createCreatorCaptionPort(),
    manualCaptions: createCreatorManualCaptionPort(),
    clipPlacement: createCreatorClipPlacementPort(),
    roughCut: createCreatorRoughCutPort(),
    timeline: createCreatorTimelinePort(),
    history: createCreatorHistoryPort(),
  };
}

function normalizeNarrativeSubtree(value: unknown): NarrativeSubtree {
  const page = asRecord(value);
  if (!Array.isArray(page.nodes)) throw new Error("Narrative subtree has invalid nodes");
  const parent = asRecord(page.parent);
  if (typeof parent.title !== "string") throw new Error("Narrative section is invalid");
  return {
    documentId: durableID(page.documentId),
    documentRevision: revisionString(page.documentRevision),
    parent: {
      id: durableID(parent.id),
      revision: revisionString(parent.revision),
      title: parent.title,
      language: canonicalLanguage(parent.language, "Narrative section"),
    },
    nodes: page.nodes.map(normalizeNarrativeNode),
    ...(typeof page.nextAfter === "string" ? { nextAfter: page.nextAfter } : {}),
    activityCursor: cursorString(page.activityCursor),
  };
}

function normalizeNarrativeNode(value: unknown): NarrativeNode {
  const node = asRecord(value);
  if (node.kind === "section" && hasOnlyNarrativePayload(node, "section")) {
    return { kind: "section", section: normalizeNarrativeSection(node.section) };
  }
  if (node.kind === "authored-text" && hasOnlyNarrativePayload(node, "authoredText")) {
    return { kind: "authored-text", authoredText: normalizeAuthoredText(node.authoredText) };
  }
  if (
    node.kind === "source-excerpt" &&
    hasOnlyNarrativePayload(node, "sourceExcerpt", true) &&
    (node.evidenceStatus === "exact" || node.evidenceStatus === "stale")
  ) {
    return {
      kind: "source-excerpt",
      sourceExcerpt: normalizeSourceExcerpt(node.sourceExcerpt),
      evidenceStatus: node.evidenceStatus,
    };
  }
  if (node.kind === "visual-intent" && hasOnlyNarrativePayload(node, "visualIntent")) {
    return { kind: "visual-intent", visualIntent: normalizeVisualIntent(node.visualIntent) };
  }
  if (node.kind === "note" && hasOnlyNarrativePayload(node, "note")) {
    return { kind: "note", note: normalizeNarrativeNote(node.note) };
  }
  throw new Error("Narrative node is invalid");
}

function hasOnlyNarrativePayload(
  node: Record<string, unknown>,
  selected: "section" | "authoredText" | "sourceExcerpt" | "visualIntent" | "note",
  allowsEvidenceStatus = false,
): boolean {
  const payloads = ["section", "authoredText", "sourceExcerpt", "visualIntent", "note"] as const;
  return (
    payloads.every((payload) => payload === selected || node[payload] === undefined) &&
    (allowsEvidenceStatus || node.evidenceStatus === undefined)
  );
}

function normalizeNarrativeSection(value: unknown): NarrativeSection {
  const node = asRecord(value);
  if (typeof node.title !== "string" || node.title.length === 0 || typeof node.tombstoned !== "boolean") {
    throw new Error("Narrative section is invalid");
  }
  return {
    id: durableID(node.id),
    revision: revisionString(node.revision),
    documentId: durableID(node.documentId),
    ...(node.parentId === undefined ? {} : { parentId: durableID(node.parentId) }),
    ...(node.afterNodeId === undefined ? {} : { afterNodeId: durableID(node.afterNodeId) }),
    title: node.title,
    language: canonicalLanguage(node.language, "Narrative section"),
    tombstoned: node.tombstoned,
  };
}

function normalizeAuthoredText(value: unknown): AuthoredText {
  const node = asRecord(value);
  if (
    typeof node.text !== "string" ||
    typeof node.tombstoned !== "boolean" ||
    (node.purpose !== "spoken" && node.purpose !== "on-screen")
  ) {
    throw new Error("authored text is invalid");
  }
  return {
    id: durableID(node.id),
    revision: revisionString(node.revision),
    documentId: durableID(node.documentId),
    parentId: durableID(node.parentId),
    ...(node.afterNodeId === undefined ? {} : { afterNodeId: durableID(node.afterNodeId) }),
    purpose: node.purpose,
    language: canonicalLanguage(node.language, "authored text"),
    text: node.text,
    tombstoned: node.tombstoned,
  };
}

function normalizeVisualIntent(value: unknown): VisualIntent {
  const node = asRecord(value);
  if (
    typeof node.description !== "string" ||
    node.description.length === 0 ||
    typeof node.tombstoned !== "boolean" ||
    (node.purpose !== "b-roll" && node.purpose !== "composition" && node.purpose !== "replacement")
  ) {
    throw new Error("visual intent is invalid");
  }
  return {
    id: durableID(node.id),
    revision: revisionString(node.revision),
    documentId: durableID(node.documentId),
    parentId: durableID(node.parentId),
    ...(node.afterNodeId === undefined ? {} : { afterNodeId: durableID(node.afterNodeId) }),
    purpose: node.purpose,
    language: canonicalLanguage(node.language, "visual intent"),
    description: node.description,
    tombstoned: node.tombstoned,
  };
}

function normalizeNarrativeNote(value: unknown): NarrativeNote {
  const node = asRecord(value);
  if (typeof node.text !== "string" || node.text.length === 0 || typeof node.tombstoned !== "boolean") {
    throw new Error("Narrative note is invalid");
  }
  return {
    id: durableID(node.id),
    revision: revisionString(node.revision),
    documentId: durableID(node.documentId),
    parentId: durableID(node.parentId),
    ...(node.afterNodeId === undefined ? {} : { afterNodeId: durableID(node.afterNodeId) }),
    language: canonicalLanguage(node.language, "Narrative note"),
    text: node.text,
    tombstoned: node.tombstoned,
  };
}

function normalizeSourceExcerpt(value: unknown): SourceExcerpt {
  const excerpt = asRecord(value);
  if (
    typeof excerpt.tombstoned !== "boolean" ||
    typeof excerpt.effectiveText !== "string" ||
    excerpt.effectiveText.length === 0 ||
    excerpt.effectiveText.length > 262_144
  ) {
    throw new Error("source excerpt is invalid");
  }
  const evidence = asRecord(excerpt.evidence);
  const segmentIds = normalizeUniqueIDs(evidence.segmentIds, "source excerpt segments", 1, 256);
  if (!Array.isArray(evidence.correctionRevisions) || evidence.correctionRevisions.length > 256) {
    throw new Error("source excerpt corrections are invalid");
  }
  const correctionRevisions = evidence.correctionRevisions.map((value) => {
    const reference = asRecord(value);
    return { id: durableID(reference.id), revision: revisionString(reference.revision) };
  });
  if (new Set(correctionRevisions.map((reference) => reference.id)).size !== correctionRevisions.length) {
    throw new Error("source excerpt corrections are not unique");
  }
  return {
    id: durableID(excerpt.id),
    revision: revisionString(excerpt.revision),
    documentId: durableID(excerpt.documentId),
    parentId: durableID(excerpt.parentId),
    ...(excerpt.afterNodeId === undefined ? {} : { afterNodeId: durableID(excerpt.afterNodeId) }),
    assetId: durableID(excerpt.assetId),
    acceptedFingerprint: digestString(excerpt.acceptedFingerprint),
    sourceRange: normalizeTimeRange(excerpt.sourceRange),
    language: canonicalLanguage(excerpt.language, "source excerpt"),
    effectiveText: excerpt.effectiveText,
    evidence: {
      artifactId: durableID(evidence.artifactId),
      sourceStreamId: durableID(evidence.sourceStreamId),
      segmentIds,
      correctionRevisions,
    },
    tombstoned: excerpt.tombstoned,
  };
}

function normalizeUniqueIDs(value: unknown, label: string, minimum: number, maximum: number): readonly DurableID[] {
  if (!Array.isArray(value) || value.length < minimum || value.length > maximum) {
    throw new Error(`${label} are invalid`);
  }
  const ids = value.map(durableID);
  if (new Set(ids).size !== ids.length) throw new Error(`${label} are not unique`);
  return ids;
}

function normalizeSequenceWindow(value: unknown): SequenceWindow {
  const page = asRecord(value);
  if (
    !Array.isArray(page.clips) ||
    !Array.isArray(page.linkGroups) ||
    !Array.isArray(page.captions) ||
    !Array.isArray(page.alignments)
  ) {
    throw new Error("Sequence window has invalid entities");
  }
  return {
    sequenceId: durableID(page.sequenceId),
    sequenceRevision: revisionString(page.sequenceRevision),
    range: normalizeTimeRange(page.range),
    clips: page.clips.map(normalizeClip),
    linkGroups: page.linkGroups.map(normalizeLinkGroup),
    captions: page.captions.map(normalizeCaption),
    alignments: page.alignments.map(normalizeAlignment),
    ...(typeof page.nextAfter === "string" ? { nextAfter: page.nextAfter } : {}),
    activityCursor: cursorString(page.activityCursor),
  };
}

function normalizeClip(value: unknown): Clip {
  const clip = asRecord(value);
  if (typeof clip.enabled !== "boolean" || typeof clip.tombstoned !== "boolean") {
    throw new Error("Clip is invalid");
  }
  return {
    id: durableID(clip.id),
    revision: revisionString(clip.revision),
    sequenceId: durableID(clip.sequenceId),
    trackId: durableID(clip.trackId),
    assetId: durableID(clip.assetId),
    sourceStreamId: durableID(clip.sourceStreamId),
    sourceRange: normalizeTimeRange(clip.sourceRange),
    timelineRange: normalizeTimeRange(clip.timelineRange),
    enabled: clip.enabled,
    ...(clip.linkGroupId === undefined ? {} : { linkGroupId: durableID(clip.linkGroupId) }),
    tombstoned: clip.tombstoned,
  };
}

function normalizeLinkGroup(value: unknown): LinkGroup {
  const group = asRecord(value);
  if (typeof group.tombstoned !== "boolean") throw new Error("Link group is invalid");
  return {
    id: durableID(group.id),
    revision: revisionString(group.revision),
    sequenceId: durableID(group.sequenceId),
    tombstoned: group.tombstoned,
  };
}

function normalizeCaption(value: unknown): Caption {
  const caption = asRecord(value);
  if (
    typeof caption.language !== "string" ||
    caption.language.length === 0 ||
    typeof caption.text !== "string" ||
    typeof caption.tombstoned !== "boolean"
  ) {
    throw new Error("Caption is invalid");
  }
  const range = normalizeTimeRange(caption.range);
  const language = canonicalLanguage(caption.language, "caption");
  const provenance = normalizeCaptionProvenance(caption.provenance);
  const provenanceStatus = normalizeCaptionProvenanceStatus(
    caption.provenanceStatus,
    provenance,
    range,
    language,
    caption.text,
  );
  return {
    id: durableID(caption.id),
    revision: revisionString(caption.revision),
    sequenceId: durableID(caption.sequenceId),
    trackId: durableID(caption.trackId),
    range,
    language,
    text: caption.text,
    provenance,
    ...(provenanceStatus === undefined ? {} : { provenanceStatus }),
    tombstoned: caption.tombstoned,
  };
}

function normalizeCaptionProvenance(value: unknown): CaptionProvenance {
  const provenance = asRecord(value);
  if (provenance.kind === "manual" && provenance.derivation === undefined) return { kind: "manual" };
  if (provenance.kind !== "transcript-derivation" || provenance.derivation === undefined) {
    throw new Error("Caption provenance is invalid");
  }
  const derivation = asRecord(provenance.derivation);
  if (
    typeof derivation.derivedText !== "string" ||
    derivation.derivedText.length === 0 ||
    derivation.derivedText.length > 262_144 ||
    !Array.isArray(derivation.correctionRevisions) ||
    derivation.correctionRevisions.length > 256
  ) {
    throw new Error("Caption derivation provenance is invalid");
  }
  const correctionRevisions = derivation.correctionRevisions.map((value) => {
    const reference = asRecord(value);
    return { id: durableID(reference.id), revision: revisionString(reference.revision) };
  });
  if (new Set(correctionRevisions.map((reference) => reference.id)).size !== correctionRevisions.length) {
    throw new Error("Caption derivation corrections are not unique");
  }
  return {
    kind: "transcript-derivation",
    derivation: {
      sourceExcerptId: durableID(derivation.sourceExcerptId),
      sourceExcerptRevision: revisionString(derivation.sourceExcerptRevision),
      assetId: durableID(derivation.assetId),
      acceptedFingerprint: digestString(derivation.acceptedFingerprint),
      transcriptArtifactId: durableID(derivation.transcriptArtifactId),
      sourceStreamId: durableID(derivation.sourceStreamId),
      segmentIds: normalizeUniqueIDs(derivation.segmentIds, "caption derivation segments", 1, 256),
      correctionRevisions,
      clipId: durableID(derivation.clipId),
      clipRevision: revisionString(derivation.clipRevision),
      clipSourceRange: normalizeTimeRange(derivation.clipSourceRange),
      clipTimelineRange: normalizeTimeRange(derivation.clipTimelineRange),
      evidenceSourceRange: normalizeTimeRange(derivation.evidenceSourceRange),
      policy: normalizeCaptionDerivationPolicy(derivation.policy),
      derivedRange: normalizeTimeRange(derivation.derivedRange),
      derivedLanguage: canonicalLanguage(derivation.derivedLanguage, "derived caption"),
      derivedText: derivation.derivedText,
    },
  };
}

function normalizeCaptionProvenanceStatus(
  value: unknown,
  provenance: CaptionProvenance,
  range: TimeRange,
  language: string,
  text: string,
): CaptionProvenanceStatus | undefined {
  if (provenance.kind === "manual") {
    if (value !== undefined) throw new Error("Manual Caption cannot carry derivation status");
    return undefined;
  }
  const status = asRecord(value);
  if (
    (status.content !== "exact" && status.content !== "modified") ||
    (status.evidence !== "exact" && status.evidence !== "stale")
  ) {
    throw new Error("Caption provenance status is invalid");
  }
  const derivation = provenance.derivation;
  const contentMatches =
    sameTimeRange(range, derivation.derivedRange) &&
    language === derivation.derivedLanguage &&
    text === derivation.derivedText;
  if ((status.content === "exact") !== contentMatches) {
    throw new Error("Caption content status does not match its derivation");
  }
  return { content: status.content, evidence: status.evidence };
}

function sameTimeRange(left: TimeRange, right: TimeRange): boolean {
  return (
    left.start.value === right.start.value &&
    left.start.scale === right.start.scale &&
    left.duration.value === right.duration.value &&
    left.duration.scale === right.duration.scale
  );
}

function normalizeAlignment(value: unknown): Alignment {
  const alignment = asRecord(value);
  if (alignment.status !== "exact" && alignment.status !== "stale" && alignment.status !== "unbound") {
    throw new Error("Alignment is invalid");
  }
  if (!Array.isArray(alignment.targets) || alignment.targets.length < 1 || alignment.targets.length > 64) {
    throw new Error("Alignment targets are invalid");
  }
  const targets = alignment.targets.map(normalizeAlignmentTarget);
  if (targets.some((target) => target.type !== targets[0]?.type)) {
    throw new Error("Alignment targets must have one semantic type");
  }
  for (let index = 1; index < targets.length; index += 1) {
    const previous = targets[index - 1];
    const current = targets[index];
    if (!previous || !current || alignmentTargetKey(previous) >= alignmentTargetKey(current)) {
      throw new Error("Alignment targets are not in canonical order");
    }
  }
  return {
    id: durableID(alignment.id),
    revision: revisionString(alignment.revision),
    narrativeNodeId: durableID(alignment.narrativeNodeId),
    narrativeNodeRevision: revisionString(alignment.narrativeNodeRevision),
    sequenceId: durableID(alignment.sequenceId),
    targets,
    status: alignment.status,
  };
}

function normalizeAlignmentTarget(value: unknown): AlignmentTarget {
  const target = asRecord(value);
  switch (target.type) {
    case "caption": {
      if (target.clip !== undefined || target.timeline !== undefined) throw new Error("Caption target is invalid");
      const caption = asRecord(target.caption);
      return {
        type: "caption",
        caption: {
          captionId: durableID(caption.captionId),
          captionRevision: revisionString(caption.captionRevision),
          localRange: normalizeTimeRange(caption.localRange),
        },
      };
    }
    case "clip": {
      if (target.caption !== undefined || target.timeline !== undefined) throw new Error("Clip target is invalid");
      const clip = asRecord(target.clip);
      return {
        type: "clip",
        clip: {
          clipId: durableID(clip.clipId),
          clipRevision: revisionString(clip.clipRevision),
          localRange: normalizeTimeRange(clip.localRange),
        },
      };
    }
    case "timeline": {
      if (target.caption !== undefined || target.clip !== undefined) throw new Error("Timeline target is invalid");
      const timeline = asRecord(target.timeline);
      return {
        type: "timeline",
        timeline: {
          sequenceRevision: revisionString(timeline.sequenceRevision),
          range: normalizeTimeRange(timeline.range),
        },
      };
    }
    default:
      throw new Error("Alignment target type is invalid");
  }
}

function alignmentTargetKey(target: AlignmentTarget): string {
  switch (target.type) {
    case "caption":
      return `caption\u0000${target.caption.captionId}\u0000${timeRangeKey(target.caption.localRange)}`;
    case "clip":
      return `clip\u0000${target.clip.clipId}\u0000${timeRangeKey(target.clip.localRange)}`;
    case "timeline":
      return `timeline\u0000${timeRangeKey(target.timeline.range)}`;
  }
}
