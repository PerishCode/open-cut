import type {
  AgentContextAttachment,
  Asset,
  CommandReceiptRef,
  DurableID,
  NarrativeNode,
  NarrativeSubtree,
  RationalTime,
  SequenceWindow,
  Track,
  TranscriptReadPage,
  TranscriptSegment,
} from "@open-cut/contracts";
import { durableID } from "@open-cut/contracts";

export type CreatorAgentContextCandidate = Readonly<{
  key: string;
  label: string;
  attachment: AgentContextAttachment;
}>;

export type WorkspaceSelection = Readonly<{ items: readonly AgentContextAttachment[] }>;

export type WorkspaceSelectionProjection = Readonly<{
  assets: readonly Asset[];
  narrative?: NarrativeSubtree;
  sequence?: SequenceWindow;
  tracks: readonly Track[];
  transcript?: TranscriptReadPage;
}>;

type FocusEntityKind = "asset" | "narrative-node" | "clip" | "caption" | "track";
type FocusObjectKind =
  | "artifact"
  | "asset-media-state"
  | "edit-proposal"
  | "edit-transaction"
  | "export-artifact"
  | "media-job"
  | "narrative-document"
  | "product-resource"
  | "project"
  | "proposal"
  | "resource-job"
  | "run"
  | "sequence-frame-artifact"
  | "sequence-frame-job"
  | "sequence-preview-job"
  | "transaction"
  | "transcript-artifact"
  | "work-job";

export type WorkspaceFocusIntent =
  | Readonly<{ kind: "entity"; entityKind: FocusEntityKind; id: DurableID; revision?: string }>
  | Readonly<{ kind: "sequence"; id: DurableID; revision?: string }>
  | Readonly<{ kind: "object"; objectKind: FocusObjectKind; id: DurableID; revision?: string }>;

export type WorkspaceFocusResult = Readonly<{
  attachment?: AgentContextAttachment;
  assetId?: DurableID;
  notice: string;
}>;

export const emptyWorkspaceSelection: WorkspaceSelection = { items: [] };

export function includeWorkspaceSelection(
  selection: WorkspaceSelection,
  attachment: AgentContextAttachment,
): WorkspaceSelection {
  const key = attachmentKey(attachment);
  return {
    items: [attachment, ...selection.items.filter((candidate) => attachmentKey(candidate) !== key)].slice(0, 64),
  };
}

export function creatorAgentContextCandidates(
  selection: WorkspaceSelection,
  projection: WorkspaceSelectionProjection,
): readonly CreatorAgentContextCandidate[] {
  return selection.items.map((attachment) => ({
    key: attachmentKey(attachment),
    label: attachmentProjectionLabel(attachment, projection),
    attachment,
  }));
}

export function assetContext(asset: Asset): AgentContextAttachment {
  return { kind: "asset", entity: { id: asset.id, revision: asset.revision } };
}

export function narrativeContext(node: NarrativeNode): AgentContextAttachment {
  const entity = narrativeEntity(node);
  return { kind: "narrative-node", entity: { id: entity.id, revision: entity.revision } };
}

export function clipContext(clip: SequenceWindow["clips"][number]): AgentContextAttachment {
  return { kind: "clip", entity: { id: clip.id, revision: clip.revision } };
}

export function captionContext(caption: SequenceWindow["captions"][number]): AgentContextAttachment {
  return { kind: "caption", entity: { id: caption.id, revision: caption.revision } };
}

export function trackContext(track: Track): AgentContextAttachment {
  return { kind: "track", entity: { id: track.id, revision: track.revision } };
}

export function transcriptSegmentContext(
  transcript: TranscriptReadPage,
  segment: TranscriptSegment,
): AgentContextAttachment {
  return {
    kind: "transcript-segment",
    transcript: { artifactId: transcript.artifact.id, segmentId: segment.id },
  };
}

export function sequencePointContext(sequence: SequenceWindow, time: RationalTime): AgentContextAttachment {
  return {
    kind: "sequence-point",
    point: { sequenceId: sequence.sequenceId, revision: sequence.sequenceRevision, time },
  };
}

export function sequenceRangeContext(sequence: SequenceWindow): AgentContextAttachment {
  return {
    kind: "sequence-range",
    range: { sequenceId: sequence.sequenceId, revision: sequence.sequenceRevision, range: sequence.range },
  };
}

export function workspaceFocusIntent(ref: CommandReceiptRef): WorkspaceFocusIntent | undefined {
  let id: DurableID;
  try {
    id = durableID(ref.id);
  } catch {
    return undefined;
  }
  const revision = ref.revision;
  if (isFocusEntityKind(ref.kind))
    return { kind: "entity", entityKind: ref.kind, id, ...(revision ? { revision } : {}) };
  if (ref.kind === "sequence") return { kind: "sequence", id, ...(revision ? { revision } : {}) };
  if (isFocusObjectKind(ref.kind))
    return { kind: "object", objectKind: ref.kind, id, ...(revision ? { revision } : {}) };
  return undefined;
}

export function resolveWorkspaceFocus(
  intent: WorkspaceFocusIntent,
  projection: WorkspaceSelectionProjection,
): WorkspaceFocusResult {
  if (intent.kind === "object") {
    return {
      notice: `Focused receipt ${intent.objectKind} ${intent.id}; this durable object is outside the current canvas projection.`,
    };
  }
  if (intent.kind === "sequence") {
    const sequence = projection.sequence;
    if (!sequence || sequence.sequenceId !== intent.id) return missingFocus("Sequence", intent.id);
    return {
      attachment: sequenceRangeContext(sequence),
      notice: revisionFocusNotice("Sequence", intent.id, intent.revision, sequence.sequenceRevision),
    };
  }
  switch (intent.entityKind) {
    case "asset": {
      const entity = projection.assets.find((candidate) => candidate.id === intent.id);
      return entity
        ? {
            attachment: assetContext(entity),
            assetId: entity.id,
            notice: revisionFocusNotice("Asset", entity.id, intent.revision, entity.revision),
          }
        : missingFocus("Asset", intent.id);
    }
    case "narrative-node": {
      const node = projection.narrative?.nodes.find((candidate) => narrativeEntity(candidate).id === intent.id);
      if (!node) return missingFocus("Narrative node", intent.id);
      const entity = narrativeEntity(node);
      return {
        attachment: narrativeContext(node),
        notice: revisionFocusNotice("Narrative node", entity.id, intent.revision, entity.revision),
      };
    }
    case "clip": {
      const entity = projection.sequence?.clips.find((candidate) => candidate.id === intent.id);
      return entity
        ? {
            attachment: clipContext(entity),
            notice: revisionFocusNotice("Clip", entity.id, intent.revision, entity.revision),
          }
        : missingFocus("Clip", intent.id);
    }
    case "caption": {
      const entity = projection.sequence?.captions.find((candidate) => candidate.id === intent.id);
      return entity
        ? {
            attachment: captionContext(entity),
            notice: revisionFocusNotice("Caption", entity.id, intent.revision, entity.revision),
          }
        : missingFocus("Caption", intent.id);
    }
    case "track": {
      const entity = projection.tracks.find((candidate) => candidate.id === intent.id);
      return entity
        ? {
            attachment: trackContext(entity),
            notice: revisionFocusNotice("Track", entity.id, intent.revision, entity.revision),
          }
        : missingFocus("Track", intent.id);
    }
  }
}

function attachmentProjectionLabel(
  attachment: AgentContextAttachment,
  projection: WorkspaceSelectionProjection,
): string {
  if ("entity" in attachment) {
    if (attachment.kind === "asset") {
      const asset = projection.assets.find((candidate) => candidate.id === attachment.entity.id);
      return `Asset · ${asset?.displayName ?? attachment.entity.id} · r${attachment.entity.revision}`;
    }
    if (attachment.kind === "track") {
      const track = projection.tracks.find((candidate) => candidate.id === attachment.entity.id);
      return `Track · ${track?.label ?? attachment.entity.id} · r${attachment.entity.revision}`;
    }
    return `${attachment.kind} · ${attachment.entity.id} · r${attachment.entity.revision}`;
  }
  if ("transcript" in attachment) {
    const segment = projection.transcript?.segments.find(
      (candidate) =>
        projection.transcript?.artifact.id === attachment.transcript.artifactId &&
        candidate.id === attachment.transcript.segmentId,
    );
    return `Transcript segment · ${segment?.text ?? attachment.transcript.segmentId}`;
  }
  if ("point" in attachment) return `Sequence point · ${formatRational(attachment.point.time)}`;
  return `Sequence range · ${formatRational(attachment.range.range.start)}`;
}

function attachmentKey(attachment: AgentContextAttachment): string {
  if ("entity" in attachment) return `${attachment.kind}:${attachment.entity.id}:${attachment.entity.revision}`;
  if ("transcript" in attachment) {
    return `${attachment.kind}:${attachment.transcript.artifactId}:${attachment.transcript.segmentId}`;
  }
  if ("point" in attachment) {
    return `${attachment.kind}:${attachment.point.sequenceId}:${attachment.point.revision}:${formatRational(attachment.point.time)}`;
  }
  return `${attachment.kind}:${attachment.range.sequenceId}:${attachment.range.revision}:${formatRational(attachment.range.range.start)}:${formatRational(attachment.range.range.duration)}`;
}

function narrativeEntity(node: NarrativeNode) {
  if (node.kind === "section") return node.section;
  if (node.kind === "authored-text") return node.authoredText;
  if (node.kind === "source-excerpt") return node.sourceExcerpt;
  if (node.kind === "visual-intent") return node.visualIntent;
  return node.note;
}

function revisionFocusNotice(kind: string, id: DurableID, historical: string | undefined, current: string): string {
  if (historical && historical !== current) {
    return `${kind} ${id}: receipt references r${historical}; current workspace is r${current}.`;
  }
  return `${kind} ${id} is focused at current revision r${current}.`;
}

function missingFocus(kind: string, id: DurableID): WorkspaceFocusResult {
  return {
    notice: `${kind} ${id} is durable receipt evidence but is not available in the current bounded projection.`,
  };
}

function formatRational(value: RationalTime): string {
  return `${value.value}/${value.scale}`;
}

function isFocusEntityKind(value: string): value is FocusEntityKind {
  return (
    value === "asset" || value === "narrative-node" || value === "clip" || value === "caption" || value === "track"
  );
}

function isFocusObjectKind(value: string): value is FocusObjectKind {
  return [
    "artifact",
    "asset-media-state",
    "edit-proposal",
    "edit-transaction",
    "export-artifact",
    "media-job",
    "narrative-document",
    "product-resource",
    "project",
    "proposal",
    "resource-job",
    "run",
    "sequence-frame-artifact",
    "sequence-frame-job",
    "sequence-preview-job",
    "transaction",
    "transcript-artifact",
    "work-job",
  ].includes(value);
}
