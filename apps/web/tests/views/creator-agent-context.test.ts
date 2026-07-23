import {
  type Asset,
  cursorString,
  durableID,
  int64String,
  type NarrativeNode,
  revisionString,
  type SequenceWindow,
  type Track,
  type TranscriptReadPage,
  type TranscriptSegment,
} from "@open-cut/contracts";
import { describe, expect, it } from "vitest";

import {
  assetContext,
  captionContext,
  clipContext,
  creatorAgentContextCandidates,
  emptyWorkspaceSelection,
  includeWorkspaceSelection,
  narrativeContext,
  resolveWorkspaceFocus,
  sequencePointContext,
  sequenceRangeContext,
  trackContext,
  transcriptSegmentContext,
  workspaceFocusIntent,
} from "../../src/components/creator-agent-context.js";

const id = (value: number) => durableID(`018f0a60-7b80-7a01-8000-${String(value).padStart(12, "0")}`);
const range = {
  start: { value: int64String("0"), scale: 1 },
  duration: { value: int64String("10"), scale: 1 },
};
const asset = { id: id(501), revision: revisionString("2"), displayName: "Interview" } as unknown as Asset;
const node = {
  kind: "authored-text",
  authoredText: { id: id(502), revision: revisionString("3"), text: "Opening" },
} as unknown as NarrativeNode;
const track = { id: id(503), revision: revisionString("4"), label: "Video 1", type: "video" } as Track;
const clip = {
  id: id(504),
  revision: revisionString("5"),
  timelineRange: range,
} as unknown as SequenceWindow["clips"][number];
const caption = {
  id: id(505),
  revision: revisionString("6"),
  text: "Hello",
  range,
} as unknown as SequenceWindow["captions"][number];
const sequence = {
  sequenceId: id(506),
  sequenceRevision: revisionString("7"),
  range,
  clips: [clip],
  captions: [caption],
  linkGroups: [],
  alignments: [],
  activityCursor: cursorString("1"),
} as unknown as SequenceWindow;
const segment = {
  id: id(508),
  text: "First sentence",
  sourceRange: range,
  ordinal: 0,
  tokens: [],
} as TranscriptSegment;
const transcript = {
  artifact: { id: id(507) },
  segments: [segment],
  corrections: [],
} as unknown as TranscriptReadPage;

describe("Creator Agent workspace context", () => {
  it("derives every closed attachment kind from one WorkspaceSelection", () => {
    const attachments = [
      assetContext(asset),
      narrativeContext(node),
      clipContext(clip),
      captionContext(caption),
      trackContext(track),
      transcriptSegmentContext(transcript, segment),
      sequencePointContext(sequence, range.start),
      sequenceRangeContext(sequence),
    ];
    const selection = attachments.reduce(includeWorkspaceSelection, emptyWorkspaceSelection);
    const candidates = creatorAgentContextCandidates(selection, {
      assets: [asset],
      narrative: { nodes: [node] } as never,
      sequence,
      tracks: [track],
      transcript,
    });
    expect(new Set(candidates.map((candidate) => candidate.attachment.kind))).toEqual(
      new Set([
        "asset",
        "narrative-node",
        "clip",
        "caption",
        "track",
        "transcript-segment",
        "sequence-point",
        "sequence-range",
      ]),
    );
    expect(candidates.some((candidate) => candidate.label.includes("Interview"))).toBe(true);
    expect(candidates.some((candidate) => candidate.label.includes("First sentence"))).toBe(true);
  });

  it("focuses current durable state while labeling a historical receipt revision", () => {
    const intent = workspaceFocusIntent({ kind: "caption", id: caption.id, revision: revisionString("5") });
    expect(intent).toBeDefined();
    if (!intent) throw new Error("caption focus intent is missing");
    const result = resolveWorkspaceFocus(intent, {
      assets: [asset],
      sequence,
      tracks: [track],
      transcript,
    });
    expect(result.attachment).toEqual(captionContext(caption));
    expect(result.notice).toContain("receipt references r5; current workspace is r6");
  });

  it("routes current Narrative document and node receipts to the Story surface", () => {
    const narrative = {
      documentId: id(509),
      documentRevision: revisionString("8"),
      nodes: [node],
    } as never;
    const documentIntent = workspaceFocusIntent({
      kind: "narrative-document",
      id: id(509),
      revision: revisionString("7"),
    });
    const nodeIntent = workspaceFocusIntent({
      kind: "narrative-node",
      id: id(502),
      revision: revisionString("2"),
    });
    expect(documentIntent).toBeDefined();
    expect(nodeIntent).toBeDefined();
    if (!documentIntent || !nodeIntent) throw new Error("Narrative focus intent is missing");

    const documentResult = resolveWorkspaceFocus(documentIntent, {
      assets: [],
      narrative,
      tracks: [],
    });
    const nodeResult = resolveWorkspaceFocus(nodeIntent, {
      assets: [],
      narrative,
      tracks: [],
    });

    expect(documentResult.sourceSurface).toBe("story");
    expect(documentResult.notice).toContain("receipt references r7; current workspace is r8");
    expect(nodeResult.sourceSurface).toBe("story");
    expect(nodeResult.attachment).toEqual(narrativeContext(node));
  });
});
