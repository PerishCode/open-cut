// @vitest-environment jsdom

import {
  type ApplyCreatorClipPlacementInput,
  ContractsProvider,
  type CreatorClipPlacementPort,
  type CreatorClipPlacementPreviewInput,
  type CreatorClipPlacementReview,
  type CreatorEditCommit,
  CreatorEditError,
  createContracts,
  digestString,
  durableID,
  int64String,
  revisionString,
  type Track,
} from "@open-cut/contracts";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { CreatorSourcePlacement } from "../../src/components/creator-source-placement.js";
import type { SequenceViewerController, SequenceViewerSnapshot } from "../../src/lib/sequence-viewer-controller.js";
import type { SourceViewerController, SourceViewerSnapshot } from "../../src/lib/source-viewer-controller.js";

const ids = {
  project: durableID("018f0a60-7b80-7a01-8000-000000000901"),
  sequence: durableID("018f0a60-7b80-7a01-8000-000000000902"),
  asset: durableID("018f0a60-7b80-7a01-8000-000000000903"),
  videoStream: durableID("018f0a60-7b80-7a01-8000-000000000904"),
  audioStream: durableID("018f0a60-7b80-7a01-8000-000000000905"),
  videoTrack: durableID("018f0a60-7b80-7a01-8000-000000000906"),
  audioTrack: durableID("018f0a60-7b80-7a01-8000-000000000907"),
  proposal: durableID("018f0a60-7b80-7a01-8000-000000000908"),
  transaction: durableID("018f0a60-7b80-7a01-8000-000000000909"),
} as const;

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("Creator source placement", () => {
  it("captures an exact destination and byte-retries only an ambiguous apply", async () => {
    const preview = vi.fn(async (input: CreatorClipPlacementPreviewInput) => placementReview(input));
    const inputs: unknown[] = [];
    const reviews: unknown[] = [];
    const apply = vi.fn(async (review: CreatorClipPlacementReview, input: ApplyCreatorClipPlacementInput) => {
      reviews.push(review);
      inputs.push(input);
      if (inputs.length === 1) throw new CreatorEditError("failed", 503);
      return commitReceipt();
    });
    const onCommitted = vi.fn(async () => undefined);
    const onShowSequence = vi.fn();
    const sourceViewer = sourceController();
    const sequenceViewer = sequenceController();
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => "018f0a60-7b80-7a01-8000-00000000090a") });

    renderPlacement({
      apply,
      onCommitted,
      onShowSequence,
      preview,
      sequenceViewer,
      sourceViewer,
    });

    expect(screen.getByText("IN −00:02.00 · 1.00s")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Selected · V1 · r5" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("button", { name: "Selected · A1 · r6" }).getAttribute("aria-pressed")).toBe("true");
    expect((screen.getByRole("button", { name: "Place source" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Capture playhead" }));
    expect(screen.getByText("AT 00:04.00 · r7")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Place at 00:04.00" }) as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Place at 00:04.00" }));

    const retry = await screen.findByRole("button", { name: "Retry identical apply" });
    expect(screen.getByText("Could not confirm the source placement.")).toBeTruthy();
    expect(screen.queryByText(/Creator edit failed|503/)).toBeNull();
    expect(preview).toHaveBeenCalledWith({
      projectId: ids.project,
      sequenceId: ids.sequence,
      assetId: ids.asset,
      assetRevision: revisionString("3"),
      acceptedFingerprint: digestString(`sha256:${"a".repeat(64)}`),
      sourceRange: range(-2, 1, 1, 1),
      timelineStart: time(4, 1),
      video: { trackId: ids.videoTrack, trackRevision: revisionString("5"), sourceStreamId: ids.videoStream },
      audio: { trackId: ids.audioTrack, trackRevision: revisionString("6"), sourceStreamId: ids.audioStream },
    });
    fireEvent.click(retry);

    await waitFor(() => expect(onCommitted).toHaveBeenCalledOnce());
    expect(inputs).toHaveLength(2);
    expect(inputs[1]).toBe(inputs[0]);
    expect(reviews[1]).toBe(reviews[0]);
    expect(sourceViewer.clearMarks).toHaveBeenCalledOnce();
    expect(sequenceViewer.setAvailableRevision).toHaveBeenCalledWith(revisionString("8"));
    expect(sequenceViewer.adoptRevision).toHaveBeenCalledWith(revisionString("8"));
    expect(sequenceViewer.setPlayhead).toHaveBeenCalledWith(time(4, 1));
    expect(onShowSequence).toHaveBeenCalledOnce();
    expect(screen.queryByRole("button", { name: "Retry identical apply" })).toBeNull();
  });

  it("does not offer mutation replay when post-commit refresh fails", async () => {
    const apply = vi.fn(async () => commitReceipt());
    const onCommitted = vi.fn(async () => {
      throw new Error("projection offline");
    });
    renderPlacement({
      apply,
      onCommitted,
      onShowSequence: vi.fn(),
      preview: vi.fn(async (input: CreatorClipPlacementPreviewInput) => placementReview(input)),
      sequenceViewer: sequenceController(),
      sourceViewer: sourceController(),
    });

    fireEvent.click(screen.getByRole("button", { name: "Capture playhead" }));
    fireEvent.click(screen.getByRole("button", { name: "Place at 00:04.00" }));

    expect(
      await screen.findByText("Source was placed, but the workspace could not refresh. Choose Sync now to reload it."),
    ).toBeTruthy();
    expect(screen.queryByText("projection offline")).toBeNull();
    expect(apply).toHaveBeenCalledOnce();
    expect(screen.queryByRole("button", { name: "Retry identical apply" })).toBeNull();
  });

  it("blocks a marked range before preview when a selected lane does not cover it", () => {
    const preview = vi.fn();
    renderPlacement({
      apply: vi.fn(),
      onCommitted: vi.fn(),
      onShowSequence: vi.fn(),
      preview,
      sequenceViewer: sequenceController(),
      sourceViewer: sourceController(false),
    });

    fireEvent.click(screen.getByRole("button", { name: "Capture playhead" }));

    expect(screen.getByText(/Marked range falls outside selected A\/V coverage/)).toBeTruthy();
    expect((screen.getByRole("button", { name: "Place at 00:04.00" }) as HTMLButtonElement).disabled).toBe(true);
    expect(preview).not.toHaveBeenCalled();
  });
});

function renderPlacement({
  apply,
  onCommitted,
  onShowSequence,
  preview,
  sequenceViewer,
  sourceViewer,
}: {
  apply: CreatorClipPlacementPort["apply"];
  onCommitted: (receipt: CreatorEditCommit) => Promise<void>;
  onShowSequence: () => void;
  preview: CreatorClipPlacementPort["preview"];
  sequenceViewer: SequenceViewerController;
  sourceViewer: SourceViewerController;
}) {
  const base = createContracts();
  const contracts = {
    ...base,
    editing: { ...base.editing, clipPlacement: { preview, apply } },
    start: () => undefined,
    close: () => undefined,
  };
  return render(
    <ContractsProvider contracts={contracts}>
      <CreatorSourcePlacement
        onCommitted={onCommitted}
        onShowSequence={onShowSequence}
        sequenceId={ids.sequence}
        sequenceSnapshot={sequenceSnapshot()}
        sequenceViewer={sequenceViewer}
        sourceSnapshot={sourceSnapshot()}
        sourceViewer={sourceViewer}
        tracks={tracks()}
      />
    </ContractsProvider>,
  );
}

function sourceController(rangeFitsCoverage = true): SourceViewerController {
  return {
    selectedRange: vi.fn(() => range(-2, 1, 1, 1)),
    selectedRangeFitsCoverage: vi.fn(() => rangeFitsCoverage),
    pause: vi.fn(),
    clearMarks: vi.fn(),
  } as unknown as SourceViewerController;
}

function sequenceController(): SequenceViewerController {
  return {
    getSnapshot: vi.fn(() => sequenceSnapshot()),
    pause: vi.fn(),
    setAvailableRevision: vi.fn(),
    adoptRevision: vi.fn(),
    setPlayhead: vi.fn(),
  } as unknown as SequenceViewerController;
}

function sourceSnapshot(): SourceViewerSnapshot {
  return {
    status: "ready",
    selection: {
      projectId: ids.project,
      assetId: ids.asset,
      assetRevision: revisionString("3"),
      fingerprint: digestString(`sha256:${"a".repeat(64)}`),
      videoStreamId: ids.videoStream,
      audioStreamId: ids.audioStream,
    },
    marks: { in: time(-2, 1), out: time(-1, 1) },
    playback: "paused",
  };
}

function sequenceSnapshot(): SequenceViewerSnapshot {
  return {
    status: "ready",
    projectId: ids.project,
    sequenceId: ids.sequence,
    pinnedRevision: revisionString("7"),
    availableRevision: revisionString("7"),
    playhead: time(4, 1),
    playback: "paused",
  };
}

function tracks(): Track[] {
  return [
    { id: ids.videoTrack, revision: revisionString("5"), type: "video", label: "V1" },
    { id: ids.audioTrack, revision: revisionString("6"), type: "audio", label: "A1" },
  ];
}

function placementReview(input: CreatorClipPlacementPreviewInput): CreatorClipPlacementReview {
  return {
    projectId: input.projectId,
    sequenceId: input.sequenceId,
    baseProjectRevision: revisionString("12"),
    activityCursor: "13" as CreatorClipPlacementReview["activityCursor"],
    outputDigest: digestString(`sha256:${"b".repeat(64)}`),
    assetId: input.assetId,
    assetRevision: input.assetRevision,
    acceptedFingerprint: input.acceptedFingerprint,
    sourceRange: input.sourceRange,
    timelineRange: { start: input.timelineStart, duration: input.sourceRange.duration },
    lanes: [
      { type: "video", trackId: ids.videoTrack, sourceStreamId: ids.videoStream },
      { type: "audio", trackId: ids.audioTrack, sourceStreamId: ids.audioStream },
    ],
    linked: true,
    preconditionCount: 4,
  };
}

function commitReceipt(): CreatorEditCommit {
  return {
    proposalId: ids.proposal,
    transactionId: ids.transaction,
    committedProjectRevision: revisionString("13"),
    activityCursor: "14" as CreatorEditCommit["activityCursor"],
    changes: [{ kind: "sequence", id: ids.sequence, revision: revisionString("8"), tombstoned: false }],
    allocation: [],
    replayed: false,
  };
}

function range(startValue: number, startScale: number, durationValue: number, durationScale: number) {
  return { start: time(startValue, startScale), duration: time(durationValue, durationScale) };
}

function time(value: number, scale: number) {
  return { value: int64String(String(value)), scale };
}
