import { afterEach, describe, expect, it, vi } from "vitest";

import { createContracts, durableID, int64String, revisionString } from "../src/index.js";
import { jsonResponse } from "./http-fixtures.js";
import { testIDs as ids } from "./test-identities.js";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("Creator Caption Contracts", () => {
  it("keeps the exact derivation envelope opaque and byte-replays one direct Creator apply", async () => {
    const previewBodies: string[] = [];
    const applyBodies: string[] = [];
    let previewPrefix = "";
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/caption-derivation-preview")) {
          const body = String(init?.body);
          previewBodies.push(body);
          previewPrefix = JSON.parse(body).localPrefix;
          return jsonResponse(captionPreview(previewPrefix));
        }
        if (url.endsWith("/edits")) {
          applyBodies.push(String(init?.body));
          if (applyBodies.length === 1) {
            return new Response(JSON.stringify({ title: "Unavailable", status: 503 }), {
              status: 503,
              headers: { "content-type": "application/problem+json" },
            });
          }
          return jsonResponse(commitReceipt(previewPrefix));
        }
        throw new Error(`unexpected request ${url}`);
      }),
    );

    const port = createContracts().editing.captions;
    const review = await port.preview(captionInput());
    const previewBody = JSON.parse(previewBodies[0] ?? "{}");
    expect(previewBody).toMatchObject({
      sourceExcerptId: ids.alphaExcerpt,
      sourceExcerptRevision: "2",
      clipId: ids.clip,
      clipRevision: "3",
      trackId: ids.caption,
      trackRevision: "4",
    });
    expect(previewBody.localPrefix).toMatch(/^cap_[a-z0-9]{32}$/);
    expect(review).toEqual({
      projectId: ids.alpha,
      sequenceId: ids.alphaSequence,
      baseProjectRevision: "8",
      activityCursor: "12",
      language: "en",
      policy: readablePolicy(),
      sourceExcerptId: ids.alphaExcerpt,
      clipId: ids.clip,
      trackId: ids.caption,
      preconditionCount: 5,
      cues: [
        {
          ordinal: 1,
          text: "A concise opening.",
          sourceRange: range(0, 1, 1, 1),
          timelineRange: range(2, 1, 1, 1),
        },
      ],
    });
    await expect(
      port.apply({ ...review }, { requestId: "ui:caption:forged", intent: "Create captions" }),
    ).rejects.toThrow("not owned by this Contracts session");

    const applyInput = { requestId: "ui:caption:create-1", intent: "Create reviewed readable captions" };
    await expect(port.apply(review, applyInput)).rejects.toMatchObject({ code: "failed", status: 503 });
    const committed = await port.apply(review, applyInput);

    expect(applyBodies).toHaveLength(2);
    expect(applyBodies[1]).toBe(applyBodies[0]);
    expect(JSON.parse(applyBodies[0] ?? "{}")).toEqual({
      requestId: applyInput.requestId,
      intent: applyInput.intent,
      baseProjectRevision: "8",
      preconditions: captionPreview(previewBody.localPrefix).preconditions,
      operations: [captionPreview(previewBody.localPrefix).operation],
    });
    expect(committed).toMatchObject({
      transactionId: ids.transaction,
      committedProjectRevision: "9",
      allocation: [
        { local: `${previewBody.localPrefix}_caption_001`, kind: "caption", id: ids.captionEntity },
        { local: `${previewBody.localPrefix}_alignment_001`, kind: "alignment", id: ids.alignment },
      ],
    });
  });

  it("rejects planner output that escapes the selected revisions or generated local closure", async () => {
    let requestBody: Record<string, string> = {};
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        requestBody = JSON.parse(String(init?.body));
        const preview = captionPreview(requestBody.localPrefix ?? "cap_invalid");
        return jsonResponse({
          ...preview,
          preconditions: preview.preconditions.map((condition) =>
            condition.kind === "clip" ? { ...condition, revision: "2" } : condition,
          ),
        });
      }),
    );

    await expect(createContracts().editing.captions.preview(captionInput())).rejects.toThrow(
      "clip revision does not match",
    );

    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        const body = JSON.parse(String(init?.body));
        const preview = captionPreview(body.localPrefix);
        return jsonResponse({
          ...preview,
          operation: {
            ...preview.operation,
            derivedCaptions: [
              { ...preview.operation.derivedCaptions[0], captionAs: `${body.localPrefix}_caption_999` },
            ],
          },
        });
      }),
    );
    await expect(createContracts().editing.captions.preview(captionInput())).rejects.toThrow(
      "derived cue does not match",
    );
  });
});

function captionInput() {
  return {
    projectId: durableID(ids.alpha),
    sequenceId: durableID(ids.alphaSequence),
    sourceExcerptId: durableID(ids.alphaExcerpt),
    sourceExcerptRevision: revisionString("2"),
    clipId: durableID(ids.clip),
    clipRevision: revisionString("3"),
    trackId: durableID(ids.caption),
    trackRevision: revisionString("4"),
  };
}

function captionPreview(localPrefix: string) {
  return {
    activityCursor: "12",
    baseProjectRevision: "8",
    language: "en",
    operation: {
      type: "derive-captions",
      narrativeNode: { id: ids.alphaExcerpt },
      clip: { id: ids.clip },
      trackId: ids.caption,
      captionPolicy: readablePolicy(),
      derivedCaptions: [
        {
          alignmentAs: `${localPrefix}_alignment_001`,
          captionAs: `${localPrefix}_caption_001`,
          sourceRange: range(0, 1, 1, 1),
          text: "A concise opening.",
          timelineRange: range(2, 1, 1, 1),
        },
      ],
    },
    preconditions: [
      { kind: "asset", id: ids.asset, revision: "5" },
      { kind: "clip", id: ids.clip, revision: "3" },
      { kind: "narrative-node", id: ids.alphaExcerpt, revision: "2" },
      { kind: "track", id: ids.caption, revision: "4" },
      { kind: "transcript-correction", id: ids.alphaTranscriptCorrection, revision: "1" },
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

function commitReceipt(localPrefix: string) {
  return {
    proposal: {
      id: ids.proposal,
      projectId: ids.alpha,
      sequenceId: ids.alphaSequence,
      requestId: "ui:caption:create-1",
      actor: { kind: "creator", creatorId: ids.creator },
      status: "applied",
      appliedTransactionId: ids.transaction,
      allocation: [
        { local: `${localPrefix}_caption_001`, kind: "caption", id: ids.captionEntity },
        { local: `${localPrefix}_alignment_001`, kind: "alignment", id: ids.alignment },
      ],
    },
    transaction: {
      id: ids.transaction,
      proposalId: ids.proposal,
      projectId: ids.alpha,
      actor: { kind: "creator", creatorId: ids.creator },
      committedProjectRevision: "9",
      changes: [
        { kind: "caption", id: ids.captionEntity, after: "1" },
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
