import { afterEach, describe, expect, it, vi } from "vitest";
import { createContracts, durableID, int64String } from "../src/index.js";
import { derivedCaptionFixture } from "./editing-fixtures.js";
import { jsonResponse } from "./http-fixtures.js";
import { testIDs as ids } from "./test-identities.js";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("editing read contracts", () => {
  it("adapts bounded Narrative and Sequence windows with unified alignments", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request) => {
        const url = String(input);
        if (url.includes(`/narratives/${ids.alphaNarrative}/subtree`)) {
          expect(url).toContain(`parentId=${ids.alphaRoot}`);
          return jsonResponse(narrativeWindow());
        }
        if (url.includes(`/sequences/${ids.alphaSequence}/window`)) {
          expect(url).toContain("startValue=0&startScale=1&durationValue=10&durationScale=1");
          return jsonResponse(sequenceWindow());
        }
        throw new Error(`unexpected request ${url}`);
      }),
    );
    const read = createContracts().editing.read;
    await expect(
      read.narrativeSubtree({
        projectId: durableID(ids.alpha),
        documentId: durableID(ids.alphaNarrative),
        parentId: durableID(ids.alphaRoot),
      }),
    ).resolves.toMatchObject({
      nodes: [
        { kind: "section", section: { id: ids.alphaSection } },
        { kind: "authored-text", authoredText: { id: ids.alphaText } },
        { kind: "source-excerpt", sourceExcerpt: { id: ids.alphaExcerpt }, evidenceStatus: "exact" },
        { kind: "visual-intent", visualIntent: { id: ids.alphaVisual } },
        { kind: "note", note: { id: ids.alphaNote } },
      ],
      documentRevision: "2",
    });
    await expect(
      read.sequenceWindow({
        projectId: durableID(ids.alpha),
        sequenceId: durableID(ids.alphaSequence),
        range: {
          start: { value: int64String("0"), scale: 1 },
          duration: { value: int64String("10"), scale: 1 },
        },
      }),
    ).resolves.toMatchObject({
      clips: [{ id: ids.clip, linkGroupId: ids.linkGroup }],
      linkGroups: [{ id: ids.linkGroup }],
      captions: [
        {
          id: ids.captionEntity,
          provenance: { kind: "transcript-derivation" },
          provenanceStatus: { content: "exact", evidence: "exact" },
        },
      ],
      alignments: [
        { status: "exact", targets: [{ type: "caption" }] },
        { status: "exact", targets: [{ type: "clip" }, { type: "clip" }] },
      ],
    });
  });
});

function narrativeWindow() {
  return {
    documentId: ids.alphaNarrative,
    documentRevision: "2",
    parent: { id: ids.alphaRoot, revision: "1", title: "Story", language: "en-US" },
    nodes: [
      {
        kind: "section",
        section: {
          id: ids.alphaSection,
          revision: "1",
          documentId: ids.alphaNarrative,
          parentId: ids.alphaRoot,
          title: "Act one",
          language: "en-US",
          tombstoned: false,
        },
      },
      {
        kind: "authored-text",
        authoredText: {
          id: ids.alphaText,
          revision: "1",
          documentId: ids.alphaNarrative,
          parentId: ids.alphaRoot,
          purpose: "spoken",
          language: "en-US",
          text: "Open on the product promise.",
          tombstoned: false,
        },
      },
      {
        kind: "source-excerpt",
        evidenceStatus: "exact",
        sourceExcerpt: {
          id: ids.alphaExcerpt,
          revision: "1",
          documentId: ids.alphaNarrative,
          parentId: ids.alphaRoot,
          afterNodeId: ids.alphaText,
          assetId: ids.asset,
          acceptedFingerprint: `sha256:${"a".repeat(64)}`,
          sourceRange: { start: { value: "0", scale: 1 }, duration: { value: "2", scale: 1 } },
          language: "en-US",
          effectiveText: "A source-backed claim.",
          evidence: {
            artifactId: ids.alphaTranscriptArtifact,
            sourceStreamId: ids.sourceAudioStream,
            segmentIds: [ids.alphaTranscriptSegment],
            correctionRevisions: [{ id: ids.alphaTranscriptCorrection, revision: "1" }],
          },
          tombstoned: false,
        },
      },
      {
        kind: "visual-intent",
        visualIntent: {
          id: ids.alphaVisual,
          revision: "1",
          documentId: ids.alphaNarrative,
          parentId: ids.alphaRoot,
          afterNodeId: ids.alphaExcerpt,
          purpose: "b-roll",
          language: "en-US",
          description: "Macro product detail over the claim.",
          tombstoned: false,
        },
      },
      {
        kind: "note",
        note: {
          id: ids.alphaNote,
          revision: "1",
          documentId: ids.alphaNarrative,
          parentId: ids.alphaRoot,
          afterNodeId: ids.alphaVisual,
          language: "en-US",
          text: "Confirm legal wording before export.",
          tombstoned: false,
        },
      },
    ],
    activityCursor: "4",
  };
}

function sequenceWindow() {
  return {
    sequenceId: ids.alphaSequence,
    sequenceRevision: "2",
    range: { start: { value: "0", scale: 1 }, duration: { value: "10", scale: 1 } },
    clips: [
      {
        id: ids.clip,
        revision: "1",
        sequenceId: ids.alphaSequence,
        trackId: ids.video,
        assetId: ids.asset,
        sourceStreamId: ids.sourceVideoStream,
        sourceRange: { start: { value: "1", scale: 1 }, duration: { value: "2", scale: 1 } },
        timelineRange: { start: { value: "0", scale: 1 }, duration: { value: "2", scale: 1 } },
        enabled: true,
        linkGroupId: ids.linkGroup,
        tombstoned: false,
      },
    ],
    linkGroups: [{ id: ids.linkGroup, revision: "1", sequenceId: ids.alphaSequence, tombstoned: false }],
    captions: [
      derivedCaptionFixture({
        captionId: ids.captionEntity,
        sequenceId: ids.alphaSequence,
        trackId: ids.caption,
        sourceExcerptId: ids.alphaExcerpt,
        assetId: ids.asset,
        transcriptArtifactId: ids.alphaTranscriptArtifact,
        sourceStreamId: ids.sourceAudioStream,
        segmentId: ids.alphaTranscriptSegment,
        correctionId: ids.alphaTranscriptCorrection,
        clipId: ids.clip,
      }),
    ],
    alignments: [captionAlignment(), clipAlignment()],
    activityCursor: "4",
  };
}

function captionAlignment() {
  return {
    id: ids.alignment,
    revision: "1",
    narrativeNodeId: ids.alphaExcerpt,
    narrativeNodeRevision: "1",
    sequenceId: ids.alphaSequence,
    targets: [
      {
        type: "caption",
        caption: {
          captionId: ids.captionEntity,
          captionRevision: "1",
          localRange: { start: { value: "0", scale: 1 }, duration: { value: "2", scale: 1 } },
        },
      },
    ],
    status: "exact",
  };
}

function clipAlignment() {
  return {
    id: ids.clipAlignment,
    revision: "1",
    narrativeNodeId: ids.alphaExcerpt,
    narrativeNodeRevision: "1",
    sequenceId: ids.alphaSequence,
    targets: [ids.clip, ids.clipSecond].map((clipId) => ({
      type: "clip",
      clip: {
        clipId,
        clipRevision: "1",
        localRange: { start: { value: "0", scale: 1 }, duration: { value: "2", scale: 1 } },
      },
    })),
    status: "exact",
  };
}
