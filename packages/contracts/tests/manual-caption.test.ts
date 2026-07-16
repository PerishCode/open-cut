import { afterEach, describe, expect, it, vi } from "vitest";

import { createContracts, durableID, int64String, revisionString } from "../src/index.js";
import { jsonResponse } from "./http-fixtures.js";
import { testIDs as ids } from "./test-identities.js";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("Creator manual Caption Contracts", () => {
  it("keeps the exact update envelope opaque and byte-replays a direct Creator apply", async () => {
    const previewBodies: string[] = [];
    const applyBodies: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/caption-gesture-preview")) {
          previewBodies.push(String(init?.body));
          return jsonResponse(updatePreview());
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

    const port = createContracts().editing.manualCaptions;
    const review = await port.preview(updateInput());

    expect(JSON.parse(previewBodies[0] ?? "{}")).toEqual({
      kind: "update",
      captionId: ids.captionEntity,
      captionRevision: "3",
      trackId: ids.caption,
      trackRevision: "4",
      range: range(2, 1, 1, 1),
      language: "en",
      text: "A concise opening.",
      alignmentHandling: "preserve-if-provable",
    });
    expect(review).toEqual({
      projectId: ids.alpha,
      sequenceId: ids.alphaSequence,
      baseProjectRevision: "8",
      activityCursor: "12",
      outputDigest: `sha256:${"d".repeat(64)}`,
      kind: "update",
      subject: {
        captionId: ids.captionEntity,
        trackId: ids.caption,
        range: range(2, 1, 1, 1),
        language: "en",
        text: "A concise opening.",
        provenance: "transcript-derivation",
      },
      alignmentEffects: [
        {
          alignmentId: ids.alignment,
          revision: "2",
          handling: "preserve-if-provable",
          targetCount: 1,
        },
      ],
      preconditionCount: 4,
    });
    await expect(
      port.apply({ ...review }, { requestId: "ui:caption:update-forged", intent: "Move Caption" }),
    ).rejects.toThrow("not owned by this Contracts session");

    const applyInput = { requestId: "ui:caption:update-1", intent: "Move one Caption in time" };
    await expect(port.apply(review, applyInput)).rejects.toMatchObject({ code: "failed", status: 503 });
    const committed = await port.apply(review, applyInput);

    expect(applyBodies).toHaveLength(2);
    expect(applyBodies[1]).toBe(applyBodies[0]);
    expect(JSON.parse(applyBodies[0] ?? "{}")).toEqual({
      requestId: applyInput.requestId,
      intent: applyInput.intent,
      baseProjectRevision: "8",
      preconditions: updatePreview().preconditions,
      operations: updatePreview().operations,
    });
    expect(committed).toMatchObject({
      transactionId: ids.transaction,
      committedProjectRevision: "9",
      changes: [
        { kind: "caption", id: ids.captionEntity, revision: "4" },
        { kind: "alignment", id: ids.alignment, revision: "3" },
      ],
    });
  });

  it("rejects remap targets outside the exact closure and binds create locals to one request", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        const preview = updatePreview();
        return jsonResponse({
          ...preview,
          operations: [
            preview.operations[0],
            {
              ...preview.operations[1],
              alignmentTargets: [{ type: "caption", caption: { id: ids.clipSecond }, localRange: range(0, 1, 1, 1) }],
            },
          ],
        });
      }),
    );
    await expect(createContracts().editing.manualCaptions.preview(updateInput())).rejects.toThrow(
      "outside the exact precondition closure",
    );

    let createBody: Record<string, unknown> = {};
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        createBody = JSON.parse(String(init?.body));
        return jsonResponse(createPreview(String(createBody.captionAs)));
      }),
    );
    const review = await createContracts().editing.manualCaptions.preview({
      projectId: durableID(ids.alpha),
      sequenceId: durableID(ids.alphaSequence),
      kind: "create",
      trackId: durableID(ids.caption),
      trackRevision: revisionString("4"),
      range: range(1, 1, 2, 1),
      language: "und",
      text: "Manual title",
    });

    expect(createBody.captionAs).toMatch(/^capm_[a-z0-9]{32}$/);
    expect(createBody).toEqual({
      kind: "create",
      captionAs: createBody.captionAs,
      trackId: ids.caption,
      trackRevision: "4",
      range: range(1, 1, 2, 1),
      language: "und",
      text: "Manual title",
    });
    expect(review).toMatchObject({
      kind: "create",
      subject: { trackId: ids.caption, provenance: "manual", text: "Manual title" },
      alignmentEffects: [],
    });
  });
});

function updateInput() {
  return {
    projectId: durableID(ids.alpha),
    sequenceId: durableID(ids.alphaSequence),
    kind: "update" as const,
    captionId: durableID(ids.captionEntity),
    captionRevision: revisionString("3"),
    trackId: durableID(ids.caption),
    trackRevision: revisionString("4"),
    range: range(2, 1, 1, 1),
    language: "en",
    text: "A concise opening.",
    alignmentHandling: "preserve-if-provable" as const,
  };
}

function updatePreview() {
  return {
    baseProjectRevision: "8",
    preconditions: [
      { kind: "alignment", id: ids.alignment, revision: "2" },
      { kind: "caption", id: ids.captionEntity, revision: "3" },
      { kind: "sequence", id: ids.alphaSequence, revision: "5" },
      { kind: "track", id: ids.caption, revision: "4" },
    ],
    operations: [
      {
        type: "update-caption",
        captionId: ids.captionEntity,
        range: range(2, 1, 1, 1),
        language: "en",
        text: "A concise opening.",
      },
      {
        type: "remap-alignment",
        alignmentId: ids.alignment,
        alignmentTargets: [{ type: "caption", caption: { id: ids.captionEntity }, localRange: range(0, 1, 1, 1) }],
      },
    ],
    kind: "update",
    subject: {
      captionId: ids.captionEntity,
      trackId: ids.caption,
      range: range(2, 1, 1, 1),
      language: "en",
      text: "A concise opening.",
      provenance: "transcript-derivation",
    },
    alignmentEffects: [
      {
        alignmentId: ids.alignment,
        revision: "2",
        handling: "preserve-if-provable",
        targetCount: 1,
      },
    ],
    outputDigest: `sha256:${"d".repeat(64)}`,
    activityCursor: "12",
  };
}

function createPreview(captionAs: string) {
  return {
    baseProjectRevision: "8",
    preconditions: [
      { kind: "sequence", id: ids.alphaSequence, revision: "5" },
      { kind: "track", id: ids.caption, revision: "4" },
    ],
    operations: [
      {
        type: "add-caption",
        createAs: captionAs,
        trackId: ids.caption,
        range: range(1, 1, 2, 1),
        language: "und",
        text: "Manual title",
      },
    ],
    kind: "create",
    subject: {
      captionAs,
      trackId: ids.caption,
      range: range(1, 1, 2, 1),
      language: "und",
      text: "Manual title",
      provenance: "manual",
    },
    alignmentEffects: [],
    outputDigest: `sha256:${"e".repeat(64)}`,
    activityCursor: "12",
  };
}

function commitReceipt() {
  return {
    proposal: {
      id: ids.proposal,
      projectId: ids.alpha,
      sequenceId: ids.alphaSequence,
      requestId: "ui:caption:update-1",
      actor: { kind: "creator", creatorId: ids.creator },
      status: "applied",
      appliedTransactionId: ids.transaction,
      allocation: [],
    },
    transaction: {
      id: ids.transaction,
      proposalId: ids.proposal,
      projectId: ids.alpha,
      actor: { kind: "creator", creatorId: ids.creator },
      committedProjectRevision: "9",
      changes: [
        { kind: "caption", id: ids.captionEntity, before: "3", after: "4" },
        { kind: "alignment", id: ids.alignment, before: "2", after: "3" },
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
