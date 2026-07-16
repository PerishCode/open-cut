// @vitest-environment jsdom

import {
  type Clip,
  ContractsProvider,
  createContracts,
  digestString,
  durableID,
  int64String,
  revisionString,
  type SourceExcerpt,
  type Track,
} from "@open-cut/contracts";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { CreatorCaptions } from "../../src/components/creator-captions.js";

const ids = {
  project: "018f0a60-7b80-7a01-8000-000000000a01",
  sequence: "018f0a60-7b80-7a01-8000-000000000a02",
  document: "018f0a60-7b80-7a01-8000-000000000a03",
  root: "018f0a60-7b80-7a01-8000-000000000a04",
  excerpt: "018f0a60-7b80-7a01-8000-000000000a05",
  asset: "018f0a60-7b80-7a01-8000-000000000a06",
  transcript: "018f0a60-7b80-7a01-8000-000000000a07",
  segment: "018f0a60-7b80-7a01-8000-000000000a08",
  stream: "018f0a60-7b80-7a01-8000-000000000a09",
  mediaTrack: "018f0a60-7b80-7a01-8000-000000000a0a",
  captionTrack: "018f0a60-7b80-7a01-8000-000000000a0b",
  clip: "018f0a60-7b80-7a01-8000-000000000a0c",
  caption: "018f0a60-7b80-7a01-8000-000000000a0d",
  alignment: "018f0a60-7b80-7a01-8000-000000000a0e",
  proposal: "018f0a60-7b80-7a01-8000-000000000a0f",
  transaction: "018f0a60-7b80-7a01-8000-000000000a10",
  creator: "018f0a60-7b80-7a01-8000-000000000a11",
} as const;

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("Creator captions", () => {
  it("reviews immutable semantic cues and byte-replays an ambiguous atomic apply", async () => {
    const previewBodies: string[] = [];
    const applyBodies: string[] = [];
    const onCommitted = vi.fn(async () => undefined);
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => "018f0a60-7b80-7a01-8000-000000000a12") });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/caption-derivation-preview")) {
          const body = String(init?.body);
          previewBodies.push(body);
          return jsonResponse(captionPreview(JSON.parse(body).localPrefix));
        }
        if (url.endsWith("/edits")) {
          applyBodies.push(String(init?.body));
          if (applyBodies.length === 1) {
            return new Response(JSON.stringify({ title: "Unavailable", status: 503 }), {
              status: 503,
              headers: { "content-type": "application/problem+json" },
            });
          }
          return jsonResponse(commitReceipt());
        }
        throw new Error(`unexpected request ${url}`);
      }),
    );
    const base = createContracts();
    const contracts = { ...base, start: () => undefined, close: () => undefined };
    render(
      <ContractsProvider contracts={contracts}>
        <CreatorCaptions
          alignments={[]}
          clips={[clip()]}
          onCommitted={onCommitted}
          onReload={async () => undefined}
          projectId={durableID(ids.project)}
          sequenceId={durableID(ids.sequence)}
          source={{ sourceExcerpt: excerpt(), evidenceStatus: "exact" }}
          tracks={tracks()}
        />
      </ContractsProvider>,
    );

    expect(screen.getByText(/Choose Clip .*recommended by SourceStream/)).toBeTruthy();
    expect(screen.getByText(/Choose Caption Track · Captions · r4/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Preview readable captions" }));

    await waitFor(() => expect(screen.getAllByText("A precise sentence.")).toHaveLength(2));
    expect(screen.getByText(/REVIEW · 1 CUES · EN · readable-captions-v1/)).toBeTruthy();
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(JSON.parse(previewBodies[0] ?? "{}")).toMatchObject({
      sourceExcerptId: ids.excerpt,
      sourceExcerptRevision: "3",
      clipId: ids.clip,
      clipRevision: "2",
      trackId: ids.captionTrack,
      trackRevision: "4",
    });

    fireEvent.click(screen.getByRole("button", { name: "Apply reviewed captions" }));
    fireEvent.click(await screen.findByRole("button", { name: "Retry identical Caption apply" }));

    await waitFor(() => expect(applyBodies).toHaveLength(2));
    expect(applyBodies[1]).toBe(applyBodies[0]);
    await waitFor(() => expect(onCommitted).toHaveBeenCalledOnce());
    expect(screen.getByText("Creator Caption transaction committed")).toBeTruthy();
  });
});

function excerpt(): SourceExcerpt {
  return {
    id: durableID(ids.excerpt),
    revision: revisionString("3"),
    documentId: durableID(ids.document),
    parentId: durableID(ids.root),
    assetId: durableID(ids.asset),
    acceptedFingerprint: digestString(`sha256:${"a".repeat(64)}`),
    sourceRange: range(1, 1, 2, 1),
    language: "en",
    effectiveText: "A precise sentence.",
    evidence: {
      artifactId: durableID(ids.transcript),
      sourceStreamId: durableID(ids.stream),
      segmentIds: [durableID(ids.segment)],
      correctionRevisions: [],
    },
    tombstoned: false,
  };
}

function clip(): Clip {
  return {
    id: durableID(ids.clip),
    revision: revisionString("2"),
    sequenceId: durableID(ids.sequence),
    trackId: durableID(ids.mediaTrack),
    assetId: durableID(ids.asset),
    sourceStreamId: durableID(ids.stream),
    sourceRange: range(0, 1, 5, 1),
    timelineRange: range(4, 1, 5, 1),
    enabled: true,
    tombstoned: false,
  };
}

function tracks(): Track[] {
  return [
    { id: durableID(ids.mediaTrack), revision: revisionString("2"), type: "audio", label: "A1" },
    { id: durableID(ids.captionTrack), revision: revisionString("4"), type: "caption", label: "Captions" },
  ];
}

function captionPreview(local: string) {
  return {
    activityCursor: "12",
    baseProjectRevision: "8",
    language: "en",
    operation: {
      type: "derive-captions",
      narrativeNode: { id: ids.excerpt },
      clip: { id: ids.clip },
      trackId: ids.captionTrack,
      captionPolicy: readablePolicy(),
      derivedCaptions: [
        {
          alignmentAs: `${local}_alignment_001`,
          captionAs: `${local}_caption_001`,
          sourceRange: range(1, 1, 2, 1),
          text: "A precise sentence.",
          timelineRange: range(5, 1, 2, 1),
        },
      ],
    },
    preconditions: [
      { kind: "asset", id: ids.asset, revision: "2" },
      { kind: "clip", id: ids.clip, revision: "2" },
      { kind: "narrative-node", id: ids.excerpt, revision: "3" },
      { kind: "track", id: ids.captionTrack, revision: "4" },
    ],
  };
}

function readablePolicy() {
  return {
    id: "readable-captions-v1",
    maximumLines: 2,
    maximumLineGraphemes: 42,
    minimumDuration: time(1, 1),
    maximumDuration: time(6, 1),
    maximumGap: time(3, 4),
    maximumReadingRate: 20,
    boundaryPolicy: "terminal-punctuation-v1",
    timingPolicy: "forward-pad-no-overlap-v1",
    unicodeSegmentationId: "unicode-egc-15.0.0-uniseg-v0.4.7",
  };
}

function commitReceipt() {
  const local = "cap_018f0a607b807a018000000000000a12";
  return {
    proposal: {
      id: ids.proposal,
      projectId: ids.project,
      sequenceId: ids.sequence,
      requestId: "ui:creator-caption",
      actor: { kind: "creator", creatorId: ids.creator },
      status: "applied",
      appliedTransactionId: ids.transaction,
      allocation: [
        { local: `${local}_caption_001`, kind: "caption", id: ids.caption },
        { local: `${local}_alignment_001`, kind: "alignment", id: ids.alignment },
      ],
    },
    transaction: {
      id: ids.transaction,
      proposalId: ids.proposal,
      projectId: ids.project,
      actor: { kind: "creator", creatorId: ids.creator },
      committedProjectRevision: "9",
      changes: [
        { kind: "caption", id: ids.caption, after: "1" },
        { kind: "alignment", id: ids.alignment, after: "1" },
      ],
    },
    activityCursor: "13",
    replayed: false,
  };
}

function range(startValue: number, startScale: number, durationValue: number, durationScale: number) {
  return { start: time(startValue, startScale), duration: time(durationValue, durationScale) };
}

function time(value: number, scale: number) {
  return { value: int64String(String(value)), scale };
}

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}
