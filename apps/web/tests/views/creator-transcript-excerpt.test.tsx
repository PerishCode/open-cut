// @vitest-environment jsdom

import {
  type Asset,
  ContractsProvider,
  createContracts,
  digestString,
  durableID,
  int64String,
  revisionString,
  type TranscriptArtifact,
  type TranscriptCorrection,
  type TranscriptReadPage,
  type TranscriptSegment,
} from "@open-cut/contracts";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  type CreatorExcerptTarget,
  CreatorTranscriptExcerpt,
} from "../../src/components/creator-transcript-excerpt.js";

const ids = {
  project: "018f0a60-7b80-7a01-8000-000000000701",
  sequence: "018f0a60-7b80-7a01-8000-000000000702",
  asset: "018f0a60-7b80-7a01-8000-000000000703",
  artifact: "018f0a60-7b80-7a01-8000-000000000704",
  stream: "018f0a60-7b80-7a01-8000-000000000705",
  segment: "018f0a60-7b80-7a01-8000-000000000706",
  token1: "018f0a60-7b80-7a01-8000-000000000707",
  token2: "018f0a60-7b80-7a01-8000-000000000708",
  token3: "018f0a60-7b80-7a01-8000-000000000709",
  correction: "018f0a60-7b80-7a01-8000-00000000070a",
  root: "018f0a60-7b80-7a01-8000-00000000070b",
  paragraph: "018f0a60-7b80-7a01-8000-00000000070c",
  excerpt: "018f0a60-7b80-7a01-8000-00000000070d",
  proposal: "018f0a60-7b80-7a01-8000-00000000070e",
  transaction: "018f0a60-7b80-7a01-8000-00000000070f",
  creator: "018f0a60-7b80-7a01-8000-000000000710",
} as const;

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("CreatorTranscriptExcerpt", () => {
  it("commits exact immutable evidence and byte-replays an ambiguous insertion", async () => {
    const bodies: string[] = [];
    const onReload = vi.fn(async () => undefined);
    const onInserted = vi.fn();
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => "018f0a60-7b80-7a01-8000-000000000711") });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        bodies.push(String(init?.body));
        if (bodies.length === 1) return new Response("unavailable", { status: 503 });
        return jsonResponse(commitReceipt());
      }),
    );
    renderExcerpt({ onInserted, onReload });

    fireEvent.click(screen.getByRole("button", { name: "Select token 1 · Hello" }));
    fireEvent.click(screen.getByRole("button", { name: "Select token 3 · world" }));
    expect(screen.getByText(/00:00\.00 → 00:02\.00 · 1 segments · 1 corrections · after opening/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Insert excerpt" }));
    fireEvent.click(await screen.findByRole("button", { name: "Retry identical excerpt insertion" }));

    await waitFor(() => expect(bodies).toHaveLength(2));
    await waitFor(() => expect(onReload).toHaveBeenCalledOnce());
    expect(bodies[1]).toBe(bodies[0]);
    expect(JSON.parse(bodies[0] ?? "{}")).toMatchObject({
      baseProjectRevision: "8",
      preconditions: [
        { kind: "narrative-node", id: ids.root, revision: "4" },
        { kind: "transcript-correction", id: ids.correction, revision: "3" },
      ],
      operations: [
        {
          type: "insert-source-excerpt",
          parentId: ids.root,
          after: { id: ids.paragraph },
          assetId: ids.asset,
          acceptedFingerprint: `sha256:${"a".repeat(64)}`,
          transcriptArtifactId: ids.artifact,
          transcriptSegmentIds: [ids.segment],
          sourceRange: { start: { value: "0", scale: 1 }, duration: { value: "2", scale: 1 } },
          language: "en",
          correctionRevisions: [{ correction: { id: ids.correction }, revision: "3" }],
        },
      ],
    });
    expect(onInserted).toHaveBeenCalledWith({
      parentId: ids.root,
      parentRevision: "5",
      afterNodeId: ids.excerpt,
      label: "after inserted excerpt",
    });
    expect(screen.getByText("Excerpt added to Story")).toBeTruthy();
    expect(onReload.mock.invocationCallOrder[0]).toBeLessThan(onInserted.mock.invocationCallOrder[0] ?? 0);
  });

  it("keeps a committed insertion successful when the follow-up Story refresh fails", async () => {
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => "018f0a60-7b80-7a01-8000-000000000711") });
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse(commitReceipt())),
    );
    const onInserted = vi.fn();
    renderExcerpt({
      onInserted,
      onReload: vi.fn(async () => {
        throw new Error("refresh unavailable");
      }),
    });

    fireEvent.click(screen.getByRole("button", { name: "Select token 1 · Hello" }));
    fireEvent.click(screen.getByRole("button", { name: "Select token 3 · world" }));
    fireEvent.click(screen.getByRole("button", { name: "Insert excerpt" }));

    expect(await screen.findByText("Excerpt added to Story · refresh reads to view it")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Retry identical excerpt insertion" })).toBeNull();
    expect(onInserted).not.toHaveBeenCalled();
  });

  it("blocks a selection that cuts through a current correction", () => {
    renderExcerpt({
      corrections: [correction(range(1, 2, 1, 1))],
      onInserted: vi.fn(),
      onReload: vi.fn(async () => undefined),
    });

    fireEvent.click(screen.getByRole("button", { name: "Select token 2 · space" }));

    expect(screen.getByText("SourceExcerpt selection cuts through a TranscriptCorrection")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Insert excerpt" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("explains the Story-first handoff before and after an unanchored selection", () => {
    renderExcerpt({
      onInserted: vi.fn(),
      onReload: vi.fn(async () => undefined),
      target: false,
    });

    expect(
      screen.getByText("Story insertion point required · choose one in Story before selecting words"),
    ).toBeTruthy();
    expect((screen.getByRole("button", { name: "Insert excerpt" }) as HTMLButtonElement).disabled).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Select token 1 · Hello" }));
    fireEvent.click(screen.getByRole("button", { name: "Select token 3 · world" }));

    expect(
      screen.getByText("Story insertion point required · choose one in Story, then reselect this range"),
    ).toBeTruthy();
    expect(screen.getByText(/1 corrections · Story insertion point not set/)).toBeTruthy();
  });

  it("preserves token evidence but blocks the stale target until Narrative is reselected", async () => {
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => "018f0a60-7b80-7a01-8000-000000000712") });
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify({ title: "Conflict", status: 409 }), { status: 409 })),
    );
    const onReload = vi.fn(async () => undefined);
    renderExcerpt({ onInserted: vi.fn(), onReload });

    fireEvent.click(screen.getByRole("button", { name: "Select token 1 · Hello" }));
    fireEvent.click(screen.getByRole("button", { name: "Select token 3 · world" }));
    fireEvent.click(screen.getByRole("button", { name: "Insert excerpt" }));

    expect(await screen.findByText("Insert conflict · token selection preserved")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Selected token 1 · Hello" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Refresh Story and reselect insertion point" }));
    expect(await screen.findByText(/Transcript selection preserved · reselect the Story insertion point/)).toBeTruthy();
    expect(screen.getByText(/1 corrections · Story insertion point not set/)).toBeTruthy();
    expect((screen.getByRole("button", { name: "Insert excerpt" }) as HTMLButtonElement).disabled).toBe(true);
    expect(onReload).toHaveBeenCalledOnce();
  });
});

function renderExcerpt({
  corrections = [correction(range(1, 1, 1, 1))],
  onInserted,
  onReload,
  target = true,
}: {
  corrections?: readonly TranscriptCorrection[];
  onInserted: CreatorExcerptTarget["onInserted"];
  onReload: CreatorExcerptTarget["onReload"];
  target?: boolean;
}) {
  const segments = transcriptSegments();
  const page: TranscriptReadPage = {
    schema: "open-cut/transcript-read/v1",
    artifact: transcriptArtifact(),
    segments,
    corrections,
    activityCursor: "7" as TranscriptReadPage["activityCursor"],
  };
  const base = createContracts();
  const contracts = { ...base, start: () => undefined, close: () => undefined };
  return render(
    <ContractsProvider contracts={contracts}>
      <CreatorTranscriptExcerpt
        asset={asset()}
        corrections={corrections}
        onContext={() => undefined}
        page={page}
        segments={segments}
        target={
          target
            ? {
                projectId: durableID(ids.project),
                sequenceId: durableID(ids.sequence),
                projectRevision: revisionString("8"),
                anchor: {
                  parentId: durableID(ids.root),
                  parentRevision: revisionString("4"),
                  afterNodeId: durableID(ids.paragraph),
                  label: "after opening",
                },
                selectionEpoch: 0,
                onCommitReceipt: () => undefined,
                onInserted,
                onReload,
              }
            : undefined
        }
      />
    </ContractsProvider>,
  );
}

function asset(): Asset {
  return {
    id: durableID(ids.asset),
    revision: revisionString("2"),
    projectId: durableID(ids.project),
    displayName: "interview.mov",
    importMode: "referenced",
    acceptedFingerprint: digestString(`sha256:${"a".repeat(64)}`),
    tombstoned: false,
    availability: "online",
    artifacts: [],
    jobs: [],
  };
}

function transcriptArtifact(): TranscriptArtifact {
  return {
    id: durableID(ids.artifact),
    assetId: durableID(ids.asset),
    sourceStreamId: durableID(ids.stream),
    recognitionProfile: "whisper-small-multilingual-v1",
    engineVersion: "1.0.0",
    engineTarget: "test",
    modelName: "whisper-small",
    modelVersion: "1",
    detectedLanguage: "en",
    sourceStartTime: time(0, 1),
    normalizedSampleCount: "96000" as TranscriptArtifact["normalizedSampleCount"],
    isDefault: true,
    createdAt: "2026-07-16T00:00:00Z",
  };
}

function transcriptSegments(): TranscriptSegment[] {
  return [
    {
      id: durableID(ids.segment),
      ordinal: 0,
      sourceRange: range(0, 1, 2, 1),
      text: "Hello world",
      tokens: [
        { id: durableID(ids.token1), sourceRange: range(0, 1, 1, 2), text: "Hello" },
        { id: durableID(ids.token2), sourceRange: range(1, 2, 1, 2), text: " " },
        { id: durableID(ids.token3), sourceRange: range(1, 1, 1, 1), text: "world" },
      ],
    },
  ];
}

function correction(sourceRange: TranscriptCorrection["sourceRange"]): TranscriptCorrection {
  return {
    id: durableID(ids.correction),
    revision: revisionString("3"),
    segmentIds: [durableID(ids.segment)],
    sourceRange,
    originalText: "world",
    effectiveText: "people",
    language: "en",
  };
}

function range(startValue: number, startScale: number, durationValue: number, durationScale: number) {
  return { start: time(startValue, startScale), duration: time(durationValue, durationScale) };
}

function time(value: number, scale: number) {
  return { value: int64String(String(value)), scale };
}

function commitReceipt() {
  return {
    proposal: {
      id: ids.proposal,
      projectId: ids.project,
      sequenceId: ids.sequence,
      requestId: "ui:creator-excerpt-insert",
      actor: { kind: "creator", creatorId: ids.creator },
      status: "applied",
      appliedTransactionId: ids.transaction,
      allocation: [{ local: "excerpt_018f0a607b807a018000000000000711", kind: "narrative-node", id: ids.excerpt }],
    },
    transaction: {
      id: ids.transaction,
      proposalId: ids.proposal,
      projectId: ids.project,
      actor: { kind: "creator", creatorId: ids.creator },
      committedProjectRevision: "9",
      changes: [
        { kind: "narrative-node", id: ids.root, before: "4", after: "5" },
        { kind: "narrative-node", id: ids.excerpt, before: "0", after: "1" },
      ],
    },
    activityCursor: "8",
    replayed: false,
  };
}

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}
