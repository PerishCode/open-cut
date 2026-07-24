// @vitest-environment jsdom

import {
  type Asset,
  ContractsProvider,
  createContracts,
  digestString,
  durableID,
  int64String,
  type NarrativeSubtree,
  revisionString,
  type SourceExcerpt,
  type Track,
} from "@open-cut/contracts";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { CreatorNarrativeWriter } from "../../src/components/creator-narrative-writer.js";
import { CreatorRoughCutPanel } from "../../src/components/creator-rough-cut.js";
import {
  type CreatorRoughCutOccurrence,
  createCreatorRoughCutOccurrence,
} from "../../src/components/creator-rough-cut-queue.js";

const ids = {
  project: "018f0a60-7b80-7a01-8000-000000000801",
  sequence: "018f0a60-7b80-7a01-8000-000000000802",
  document: "018f0a60-7b80-7a01-8000-000000000803",
  root: "018f0a60-7b80-7a01-8000-000000000804",
  excerpt: "018f0a60-7b80-7a01-8000-000000000805",
  asset: "018f0a60-7b80-7a01-8000-000000000806",
  transcript: "018f0a60-7b80-7a01-8000-000000000807",
  segment: "018f0a60-7b80-7a01-8000-000000000808",
  videoStream: "018f0a60-7b80-7a01-8000-000000000809",
  videoStream2: "018f0a60-7b80-7a01-8000-00000000080a",
  audioStream: "018f0a60-7b80-7a01-8000-00000000080b",
  videoTrack: "018f0a60-7b80-7a01-8000-00000000080c",
  audioTrack: "018f0a60-7b80-7a01-8000-00000000080d",
  clip: "018f0a60-7b80-7a01-8000-00000000080e",
  alignment: "018f0a60-7b80-7a01-8000-00000000080f",
  proposal: "018f0a60-7b80-7a01-8000-000000000810",
  transaction: "018f0a60-7b80-7a01-8000-000000000811",
  creator: "018f0a60-7b80-7a01-8000-000000000812",
} as const;

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("Creator rough cut", () => {
  it("prefills a unique lane but requires an explicit choice for multiple compatible streams", () => {
    const occurrence = createCreatorRoughCutOccurrence(sourceExcerpt(), "exact", [asset(true)], tracks());

    expect(occurrence.audio).toMatchObject({
      state: "selected",
      candidate: { trackId: ids.audioTrack, sourceStreamId: ids.audioStream },
    });
    expect(occurrence.video).toEqual({ state: "unresolved" });
    expect(occurrence.videoCandidates).toHaveLength(2);
  });

  it("exposes exact SourceExcerpt occurrences from Narrative without changing the Narrative anchor", () => {
    const onAddToRoughCut = vi.fn();
    const onCreateCaptions = vi.fn();
    renderWithContracts(
      <CreatorNarrativeWriter
        narrative={narrative()}
        onAddToRoughCut={onAddToRoughCut}
        onCreateCaptions={onCreateCaptions}
        onReload={async () => undefined}
        onSelect={() => undefined}
        projectId={durableID(ids.project)}
        projectRevision={revisionString("8")}
        recentlyAddedNodeId={durableID(ids.excerpt)}
        sequenceId={durableID(ids.sequence)}
      />,
    );

    expect(screen.getByText("Added from Transcript")).toBeTruthy();
    expect(screen.getByRole("region", { name: "Story node 1 actions" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Add excerpt to rough cut" }));

    expect(onAddToRoughCut).toHaveBeenCalledWith(sourceExcerpt(), "exact");
    fireEvent.click(screen.getByRole("button", { name: "Create captions from excerpt" }));
    expect(onCreateCaptions).toHaveBeenCalledWith(sourceExcerpt(), "exact");
  });

  it("reviews the outcome, byte-replays an ambiguous apply, and preserves commit success", async () => {
    const previewBodies: string[] = [];
    const applyBodies: string[] = [];
    const onCommitted = vi.fn(async () => {
      throw new Error("projection refresh failed");
    });
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => "018f0a60-7b80-7a01-8000-000000000813") });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/rough-cut-preview")) {
          previewBodies.push(String(init?.body));
          return jsonResponse(roughCutPreview());
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
    renderWithContracts(<RoughCutHarness onCommitted={onCommitted} />);

    expect(screen.getByRole("region", { name: "Rough cut queue start" }).textContent).toContain(
      "ROUGH CUT · EXCERPT QUEUE",
    );
    expect(screen.getByRole("region", { name: "Rough cut review" }).textContent).toContain("REVIEW · 1 READY");
    expect(screen.getByRole("button", { name: "Start at current playhead · 00:00.00" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Omit video" }));
    fireEvent.click(screen.getByRole("button", { name: "Review rough cut" }));

    expect(await screen.findByText(/01 · 00:05\.00 → 00:07\.00 · A/)).toBeTruthy();
    expect(screen.getAllByText("A precise sentence.")).toHaveLength(2);
    expect(screen.getByText("Nothing changes until you add this rough cut to the Timeline.")).toBeTruthy();
    expect(screen.queryByText(/preconditions|paper-edit|OUTPUT DIGEST|GHOST/)).toBeNull();
    expect(JSON.parse(previewBodies[0] ?? "{}")).toMatchObject({
      timelineStart: { value: "5", scale: 1 },
      items: [
        {
          sourceExcerptId: ids.excerpt,
          sourceExcerptRevision: "3",
          audio: { trackId: ids.audioTrack, trackRevision: "4", sourceStreamId: ids.audioStream },
        },
      ],
    });

    fireEvent.click(screen.getByRole("button", { name: "Add rough cut to Timeline" }));
    const retry = await screen.findByRole("button", { name: "Retry same rough cut" });
    expect(screen.getByText("Could not confirm the Rough cut update.")).toBeTruthy();
    expect(screen.queryByText(/Creator edit failed|503|Unavailable/)).toBeNull();
    fireEvent.click(retry);

    await waitFor(() => expect(applyBodies).toHaveLength(2));
    await waitFor(() => expect(onCommitted).toHaveBeenCalledOnce());
    expect(applyBodies[1]).toBe(applyBodies[0]);
    expect(screen.getByText("Rough cut added · use Sync now to reveal it")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Retry same rough cut" })).toBeNull();
  });
});

function RoughCutHarness({ onCommitted }: { onCommitted(): Promise<void> }) {
  const [occurrences, setOccurrences] = useState<readonly CreatorRoughCutOccurrence[]>(() => [
    createCreatorRoughCutOccurrence(sourceExcerpt(), "exact", [asset(false)], tracks()),
  ]);
  return (
    <CreatorRoughCutPanel
      assets={[asset(false)]}
      currentPlayhead={time(0, 1)}
      occurrences={occurrences}
      onChange={setOccurrences}
      onCommitted={onCommitted}
      onReload={async () => undefined}
      onTimelineStartChange={() => undefined}
      projectId={durableID(ids.project)}
      projectRevision={revisionString("8")}
      sequenceId={durableID(ids.sequence)}
      sequenceRevision={revisionString("5")}
      timelineStart={time(5, 1)}
      tracks={tracks()}
    />
  );
}

function renderWithContracts(value: ReactNode) {
  const base = createContracts();
  const contracts = { ...base, start: () => undefined, close: () => undefined };
  return render(<ContractsProvider contracts={contracts}>{value}</ContractsProvider>);
}

function sourceExcerpt(): SourceExcerpt {
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
      sourceStreamId: durableID(ids.audioStream),
      segmentIds: [durableID(ids.segment)],
      correctionRevisions: [],
    },
    tombstoned: false,
  };
}

function narrative(): NarrativeSubtree {
  return {
    documentId: durableID(ids.document),
    documentRevision: revisionString("3"),
    parent: { id: durableID(ids.root), revision: revisionString("4"), title: "Story", language: "en" },
    nodes: [{ kind: "source-excerpt", sourceExcerpt: sourceExcerpt(), evidenceStatus: "exact" }],
    activityCursor: "7" as NarrativeSubtree["activityCursor"],
  };
}

function asset(includeSecondVideo: boolean): Asset {
  return {
    id: durableID(ids.asset),
    revision: revisionString("2"),
    projectId: durableID(ids.project),
    displayName: "interview.mov",
    importMode: "referenced",
    acceptedFingerprint: digestString(`sha256:${"a".repeat(64)}`),
    tombstoned: false,
    availability: "online",
    facts: {
      container: "matroska",
      containerAliases: [],
      streams: [
        stream(ids.videoStream, 0, "video", "vp9"),
        ...(includeSecondVideo ? [stream(ids.videoStream2, 1, "video", "vp9")] : []),
        stream(ids.audioStream, 2, "audio", "opus"),
      ],
    },
    artifacts: [],
    jobs: [],
  };
}

function stream(id: string, index: number, mediaType: "video" | "audio", codec: string) {
  return {
    id: durableID(id),
    descriptor: { index, mediaType, codec, timeBase: time(1, 48_000), dispositions: [] },
  };
}

function tracks(): Track[] {
  return [
    { id: durableID(ids.videoTrack), revision: revisionString("2"), type: "video", label: "V1" },
    { id: durableID(ids.audioTrack), revision: revisionString("4"), type: "audio", label: "A1" },
  ];
}

function roughCutPreview() {
  const local = "rough_018f0a607b807a018000000000000813";
  return {
    baseProjectRevision: "8",
    preconditions: [
      { kind: "asset", id: ids.asset, revision: "2" },
      { kind: "narrative-node", id: ids.excerpt, revision: "3" },
      { kind: "sequence", id: ids.sequence, revision: "5" },
      { kind: "track", id: ids.audioTrack, revision: "4" },
    ],
    operation: {
      type: "derive-rough-cut",
      roughCutPolicy: {
        id: "paper-edit-rough-cut-v1",
        ordering: "request-order",
        interExcerptGap: time(0, 1),
        sourceHandles: "zero",
        rate: "1:1",
        overwrite: "forbidden",
        avGrouping: "one-link-group-per-two-lane-excerpt",
      },
      roughCutTimelineStart: time(5, 1),
      roughCutLocalPrefix: local,
      roughCutItems: [
        {
          sourceExcerptId: ids.excerpt,
          audio: { trackId: ids.audioTrack, sourceStreamId: ids.audioStream },
        },
      ],
      derivedRoughCut: [
        {
          sourceExcerptId: ids.excerpt,
          sourceRange: range(1, 1, 2, 1),
          timelineRange: range(5, 1, 2, 1),
          audio: { clipAs: `${local}_audio_001`, trackId: ids.audioTrack, sourceStreamId: ids.audioStream },
          alignmentAs: `${local}_alignment_001`,
        },
      ],
      roughCutOutputDigest: `sha256:${"b".repeat(64)}`,
    },
    outputDigest: `sha256:${"b".repeat(64)}`,
    activityCursor: "12",
  };
}

function commitReceipt() {
  const local = "rough_018f0a607b807a018000000000000813";
  return {
    proposal: {
      id: ids.proposal,
      projectId: ids.project,
      sequenceId: ids.sequence,
      requestId: "ui:creator-rough-cut-apply",
      actor: { kind: "creator", creatorId: ids.creator },
      status: "applied",
      appliedTransactionId: ids.transaction,
      allocation: [
        { local: `${local}_audio_001`, kind: "clip", id: ids.clip },
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
        { kind: "clip", id: ids.clip, after: "1" },
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
