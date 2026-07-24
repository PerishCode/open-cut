// @vitest-environment jsdom

import {
  type Caption,
  ContractsProvider,
  type CreatorEditCommit,
  createContracts,
  durableID,
  int64String,
  revisionString,
  type Track,
} from "@open-cut/contracts";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ManualCaptionEditor } from "../../src/components/manual-caption-editor.js";
import { SequenceViewerController } from "../../src/lib/sequence-viewer-controller.js";

const ids = {
  project: "018f0a60-7b80-7a01-8000-000000000c01",
  sequence: "018f0a60-7b80-7a01-8000-000000000c02",
  track: "018f0a60-7b80-7a01-8000-000000000c03",
  caption: "018f0a60-7b80-7a01-8000-000000000c04",
  proposal: "018f0a60-7b80-7a01-8000-000000000c05",
  transaction: "018f0a60-7b80-7a01-8000-000000000c06",
  creator: "018f0a60-7b80-7a01-8000-000000000c07",
} as const;

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("Manual Caption editor", () => {
  it("captures explicit Viewer In/Out marks and publishes one create receipt to Workspace", async () => {
    const previewBodies: Record<string, unknown>[] = [];
    const onCommitted = vi.fn(async () => undefined);
    let applyAttempts = 0;
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => "018f0a60-7b80-7a01-8000-000000000c08") });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/caption-gesture-preview")) {
          const body = JSON.parse(String(init?.body));
          previewBodies.push(body);
          return jsonResponse(createPreview(body));
        }
        if (url.endsWith("/edits")) {
          applyAttempts += 1;
          if (applyAttempts === 1) {
            return new Response(JSON.stringify({ title: "Unavailable", status: 503 }), {
              status: 503,
              headers: { "content-type": "application/problem+json" },
            });
          }
          return jsonResponse(commitReceipt("caption", false));
        }
        throw new Error(`unexpected request ${url}`);
      }),
    );
    const base = createContracts();
    const viewer = new SequenceViewerController(base.media.viewer);
    viewer.setPlayhead(time(2));
    renderEditor(base, viewer, [], onCommitted);

    fireEvent.click(screen.getByRole("button", { name: "New manual Caption" }));
    expect(screen.getByLabelText("Caption language").getAttribute("placeholder")).toBe("Language · AUTO");
    expect((screen.getByLabelText("Caption language") as HTMLInputElement).value).toBe("");
    expect(screen.queryByDisplayValue("und")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Cancel manual Caption draft" }));
    expect(screen.queryByLabelText("Caption text")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "New manual Caption" }));
    fireEvent.change(screen.getByLabelText("Caption text"), { target: { value: "Manual title" } });
    fireEvent.click(screen.getByRole("button", { name: "Capture In at Viewer playhead" }));
    viewer.setPlayhead(time(5));
    fireEvent.click(screen.getByRole("button", { name: "Capture Out at Viewer playhead" }));
    fireEvent.click(screen.getByRole("button", { name: "Create manual Caption" }));

    const retry = await screen.findByRole("button", { name: "Retry identical Caption apply" });
    expect(screen.getByText("Could not confirm the Caption update.")).toBeTruthy();
    expect(screen.queryByText(/Creator edit failed|503|Unavailable/)).toBeNull();
    fireEvent.click(retry);

    await waitFor(() => expect(onCommitted).toHaveBeenCalledOnce());
    expect(applyAttempts).toBe(2);
    expect(previewBodies[0]).toMatchObject({
      kind: "create",
      trackId: ids.track,
      trackRevision: "4",
      range: range(2, 3),
      language: "und",
      text: "Manual title",
    });
    expect(screen.getByText("Caption transaction committed to Workspace history")).toBeTruthy();
  });

  it("keeps changed content local until the Creator chooses stale or unbind", async () => {
    vi.useFakeTimers();
    const previewBodies: Record<string, unknown>[] = [];
    const onCommitted = vi.fn(async () => undefined);
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => "018f0a60-7b80-7a01-8000-000000000c09") });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/caption-gesture-preview")) {
          const body = JSON.parse(String(init?.body));
          previewBodies.push(body);
          return jsonResponse(updatePreview());
        }
        if (url.endsWith("/edits")) return jsonResponse(commitReceipt("caption", false));
        throw new Error(`unexpected request ${url}`);
      }),
    );
    const base = createContracts();
    const viewer = new SequenceViewerController(base.media.viewer);
    renderEditor(base, viewer, [caption()], onCommitted);

    const cue = screen.getByRole("region", { name: `Caption ${ids.caption} actions` });
    expect(within(cue).getByText("Original wording")).toBeTruthy();
    expect(within(cue).getByText(/00:02.00 → 00:05.00 · r3 · MANUAL/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: `Edit Caption ${ids.caption}` }));
    fireEvent.change(screen.getByLabelText("Caption text"), { target: { value: "Creator-polished wording" } });
    expect(screen.getByText("Choose stale or unbind before checkpointing changed content")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Save Caption checkpoint" }) as HTMLButtonElement).disabled).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Mark dependent Alignments stale" }));
    expect(onCommitted).not.toHaveBeenCalled();
    await act(async () => vi.advanceTimersByTimeAsync(750));

    expect(onCommitted).toHaveBeenCalledOnce();
    expect(previewBodies[0]).toMatchObject({
      kind: "update",
      captionId: ids.caption,
      captionRevision: "3",
      text: "Creator-polished wording",
      alignmentHandling: "mark-stale",
    });
  });
});

function renderEditor(
  base: ReturnType<typeof createContracts>,
  viewer: SequenceViewerController,
  captions: readonly Caption[],
  onCommitted: (receipt: CreatorEditCommit) => Promise<void>,
) {
  const contracts = { ...base, start: () => undefined, close: () => undefined };
  return render(
    <ContractsProvider contracts={contracts}>
      <ManualCaptionEditor
        captions={captions}
        onCommitted={onCommitted}
        onContextCaption={() => undefined}
        onReload={async () => undefined}
        projectId={durableID(ids.project)}
        sequenceId={durableID(ids.sequence)}
        tracks={[track()]}
        viewer={viewer}
      />
    </ContractsProvider>,
  );
}

function caption(): Caption {
  return {
    id: durableID(ids.caption),
    revision: revisionString("3"),
    sequenceId: durableID(ids.sequence),
    trackId: durableID(ids.track),
    range: range(2, 3),
    language: "en",
    text: "Original wording",
    provenance: { kind: "manual" },
    tombstoned: false,
  };
}

function track(): Track {
  return { id: durableID(ids.track), revision: revisionString("4"), type: "caption", label: "Captions" };
}

function createPreview(body: Record<string, unknown>) {
  return {
    baseProjectRevision: "8",
    preconditions: [
      { kind: "sequence", id: ids.sequence, revision: "5" },
      { kind: "track", id: ids.track, revision: "4" },
    ],
    operations: [
      {
        type: "add-caption",
        createAs: body.captionAs,
        trackId: ids.track,
        range: range(2, 3),
        language: "und",
        text: "Manual title",
      },
    ],
    kind: "create",
    subject: {
      captionAs: body.captionAs,
      trackId: ids.track,
      range: range(2, 3),
      language: "und",
      text: "Manual title",
      provenance: "manual",
    },
    alignmentEffects: [],
    outputDigest: `sha256:${"a".repeat(64)}`,
    activityCursor: "12",
  };
}

function updatePreview() {
  return {
    baseProjectRevision: "8",
    preconditions: [
      { kind: "caption", id: ids.caption, revision: "3" },
      { kind: "sequence", id: ids.sequence, revision: "5" },
      { kind: "track", id: ids.track, revision: "4" },
    ],
    operations: [
      {
        type: "update-caption",
        captionId: ids.caption,
        range: range(2, 3),
        language: "en",
        text: "Creator-polished wording",
      },
    ],
    kind: "update",
    subject: {
      captionId: ids.caption,
      trackId: ids.track,
      range: range(2, 3),
      language: "en",
      text: "Creator-polished wording",
      provenance: "manual",
    },
    alignmentEffects: [],
    outputDigest: `sha256:${"b".repeat(64)}`,
    activityCursor: "12",
  };
}

function commitReceipt(kind: "caption", tombstoned: boolean) {
  return {
    proposal: {
      id: ids.proposal,
      projectId: ids.project,
      sequenceId: ids.sequence,
      requestId: "ui:creator-caption",
      actor: { kind: "creator", creatorId: ids.creator },
      status: "applied",
      appliedTransactionId: ids.transaction,
      allocation: [],
    },
    transaction: {
      id: ids.transaction,
      proposalId: ids.proposal,
      projectId: ids.project,
      actor: { kind: "creator", creatorId: ids.creator },
      committedProjectRevision: "9",
      changes: [{ kind, id: ids.caption, before: "3", after: "4", tombstoned }],
    },
    activityCursor: "13",
    replayed: false,
  };
}

function range(start: number, duration: number) {
  return { start: time(start), duration: time(duration) };
}

function time(value: number) {
  return { value: int64String(String(value)), scale: 1 };
}

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
}
