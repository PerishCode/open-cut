// @vitest-environment jsdom

import {
  type Asset,
  ContractsProvider,
  type CreatorEditCommit,
  type CreatorTimelineGestureReview,
  type CreatorTimelinePort,
  createContracts,
  cursorString,
  digestString,
  durableID,
  int64String,
  revisionString,
  type Track,
} from "@open-cut/contracts";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { useEffect } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CreatorTimeline } from "../../src/components/creator-timeline.js";
import { useCreatorTimelineHandoff } from "../../src/components/creator-timeline-handoff.js";
import { SequenceViewerController } from "../../src/lib/sequence-viewer-controller.js";

const ids = {
  project: durableID("018f0a60-7b80-7a01-8000-000000000d01"),
  sequence: durableID("018f0a60-7b80-7a01-8000-000000000d02"),
  track: durableID("018f0a60-7b80-7a01-8000-000000000d03"),
  clip: durableID("018f0a60-7b80-7a01-8000-000000000d04"),
  asset: durableID("018f0a60-7b80-7a01-8000-000000000d05"),
  stream: durableID("018f0a60-7b80-7a01-8000-000000000d06"),
  proposal: durableID("018f0a60-7b80-7a01-8000-000000000d07"),
  transaction: durableID("018f0a60-7b80-7a01-8000-000000000d08"),
} as const;

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("Creator Timeline handoff", () => {
  it("keeps Timeline apply failures private and retries the identical edit", async () => {
    let applyAttempts = 0;
    const timeline: CreatorTimelinePort = {
      preview: vi.fn(async (input) => ({
        status: "ready" as const,
        review: timelineReview(input.kind, input.scope),
      })),
      apply: vi.fn(async () => {
        applyAttempts += 1;
        if (applyAttempts === 1) {
          throw new Error("socket closed near /Users/editor/Library/Application Support/Open Cut/project.db");
        }
        return { commit: commitReceipt() };
      }),
    };
    const base = createContracts();
    const contracts = {
      ...base,
      editing: { ...base.editing, timeline },
      start: () => undefined,
      close: () => undefined,
    };
    const viewer = new SequenceViewerController(base.media.viewer);
    render(
      <ContractsProvider contracts={contracts}>
        <CreatorTimeline
          assets={[asset()]}
          captions={[]}
          clips={[clip()]}
          frameRate={time(30)}
          onCommitted={async () => undefined}
          onContextClip={() => undefined}
          onHandoffSeen={() => undefined}
          onReload={async () => undefined}
          projectId={ids.project}
          range={range(0, 60)}
          sequenceId={ids.sequence}
          sequenceRevision={revisionString("2")}
          tracks={[track()]}
          viewer={viewer}
        />
      </ContractsProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Select interview.mov on V1 at 00:00.00" }));
    const policy = screen.getByRole("region", { name: "Timeline selection policy" });
    fireEvent.click(within(policy).getByRole("button", { name: "Mark stale" }));
    fireEvent.click(within(policy).getByRole("button", { name: "Move here" }));

    const retry = await screen.findByRole("button", { name: "Retry identical Timeline apply" });
    expect(screen.getByText("Could not confirm the Timeline update.")).toBeTruthy();
    expect(screen.queryByText(/socket|Application Support|project\.db/i)).toBeNull();
    fireEvent.click(retry);

    await waitFor(() => expect(applyAttempts).toBe(2));
    expect(screen.getByText("Timeline transaction committed")).toBeTruthy();
  });

  it("highlights committed rough-cut clips until the Creator starts a Timeline selection", async () => {
    const base = createContracts();
    const contracts = { ...base, start: () => undefined, close: () => undefined };
    const viewer = new SequenceViewerController(base.media.viewer);
    render(
      <ContractsProvider contracts={contracts}>
        <HandoffHarness viewer={viewer} />
      </ContractsProvider>,
    );

    expect(await screen.findByText("Rough cut added · 1 clip highlighted")).toBeTruthy();
    const clipButton = screen.getByRole("button", { name: "Select interview.mov on V1 at 00:00.00" });
    expect(clipButton.getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(clipButton);
    expect(screen.queryByText("Rough cut added · 1 clip highlighted")).toBeNull();
    expect(clipButton.getAttribute("aria-pressed")).toBe("true");
    const policy = screen.getByRole("region", { name: "Timeline selection policy" });
    const move = within(policy).getByRole("button", { name: "Move here" });
    expect((move as HTMLButtonElement).disabled).toBe(true);
    expect((within(policy).getByRole("button", { name: "Trim in" }) as HTMLButtonElement).disabled).toBe(true);
    expect((within(policy).getByRole("button", { name: "Trim out" }) as HTMLButtonElement).disabled).toBe(true);
    expect((within(policy).getByRole("button", { name: "Split" }) as HTMLButtonElement).disabled).toBe(true);
    expect((within(policy).getByRole("button", { name: "Remove" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(within(policy).getByRole("button", { name: "Mark stale" }));
    expect((move as HTMLButtonElement).disabled).toBe(false);
    expect((within(policy).getByRole("button", { name: "Remove" }) as HTMLButtonElement).disabled).toBe(false);
  });
});

function HandoffHarness({ viewer }: { viewer: SequenceViewerController }) {
  const handoff = useCreatorTimelineHandoff();
  useEffect(() => handoff.revealRoughCut(commitReceipt(), true), [handoff.revealRoughCut]);
  return (
    <CreatorTimeline
      assets={[asset()]}
      captions={[]}
      clips={[clip()]}
      frameRate={time(30)}
      handoff={handoff.current}
      onCommitted={async () => undefined}
      onContextClip={() => undefined}
      onHandoffSeen={handoff.clear}
      onReload={async () => undefined}
      projectId={ids.project}
      range={range(0, 60)}
      sequenceId={ids.sequence}
      sequenceRevision={revisionString("2")}
      tracks={[track()]}
      viewer={viewer}
    />
  );
}

function asset(): Asset {
  return {
    id: ids.asset,
    revision: revisionString("1"),
    projectId: ids.project,
    displayName: "interview.mov",
    importMode: "referenced",
    acceptedFingerprint: digestString(`sha256:${"a".repeat(64)}`),
    tombstoned: false,
    availability: "online",
    artifacts: [],
    jobs: [],
  };
}

function clip() {
  return {
    id: ids.clip,
    revision: revisionString("1"),
    sequenceId: ids.sequence,
    trackId: ids.track,
    assetId: ids.asset,
    sourceStreamId: ids.stream,
    sourceRange: range(0, 4),
    timelineRange: range(0, 4),
    enabled: true,
    tombstoned: false,
  };
}

function track(): Track {
  return { id: ids.track, revision: revisionString("1"), type: "video", label: "V1" };
}

function commitReceipt(): CreatorEditCommit {
  return {
    proposalId: ids.proposal,
    transactionId: ids.transaction,
    committedProjectRevision: revisionString("3"),
    activityCursor: "8" as CreatorEditCommit["activityCursor"],
    changes: [{ kind: "clip", id: ids.clip, revision: revisionString("1"), tombstoned: false }],
    allocation: [{ local: "rough_cut_video_001", kind: "clip", id: ids.clip }],
    replayed: false,
  };
}

function timelineReview(
  kind: "move" | "trim" | "split" | "remove",
  scope: "single" | "linked",
): CreatorTimelineGestureReview {
  return {
    projectId: ids.project,
    sequenceId: ids.sequence,
    baseProjectRevision: revisionString("2"),
    activityCursor: cursorString("7"),
    outputDigest: digestString(`sha256:${"b".repeat(64)}`),
    kind,
    scope,
    seedClipId: ids.clip,
    affectedClipIds: [ids.clip],
    createdClipCount: 0,
    clipEffects: [
      {
        clipId: ids.clip,
        before: {
          revision: revisionString("1"),
          trackId: ids.track,
          sourceRange: range(0, 4),
          timelineRange: range(0, 4),
          linked: false,
        },
        outcome: "updated",
        after: {
          revision: revisionString("2"),
          trackId: ids.track,
          sourceRange: range(0, 4),
          timelineRange: range(0, 4),
          linked: false,
        },
      },
    ],
    alignmentEffects: [],
    preconditionCount: 3,
  };
}

function range(start: number, duration: number) {
  return { start: time(start), duration: time(duration) };
}

function time(value: number) {
  return { value: int64String(String(value)), scale: 1 };
}
