import { afterEach, describe, expect, it, vi } from "vitest";

import { createContracts, durableID, int64String, revisionString } from "../src/index.js";
import { jsonResponse } from "./http-fixtures.js";
import { testIDs as ids } from "./test-identities.js";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("Creator rough-cut Contracts", () => {
  it("keeps preview bytes opaque and byte-replays one direct Creator apply", async () => {
    const previewBodies: string[] = [];
    const applyBodies: string[] = [];
    vi.stubGlobal("crypto", { randomUUID: vi.fn(() => "018f0a60-7b80-7a01-8000-000000000801") });
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
    const port = createContracts().editing.roughCut;
    const review = await port.preview({
      projectId: durableID(ids.alpha),
      sequenceId: durableID(ids.alphaSequence),
      timelineStart: time(0, 1),
      items: [
        {
          sourceExcerptId: durableID(ids.alphaExcerpt),
          sourceExcerptRevision: revisionString("3"),
          audio: {
            trackId: durableID(ids.audio),
            trackRevision: revisionString("4"),
            sourceStreamId: durableID(ids.sourceAudioStream),
          },
        },
      ],
    });

    expect(JSON.parse(previewBodies[0] ?? "{}")).toEqual({
      timelineStart: { value: "0", scale: 1 },
      localPrefix: "rough_018f0a607b807a018000000000000801",
      items: [
        {
          sourceExcerptId: ids.alphaExcerpt,
          sourceExcerptRevision: "3",
          audio: { trackId: ids.audio, trackRevision: "4", sourceStreamId: ids.sourceAudioStream },
        },
      ],
    });
    expect(review).toMatchObject({
      projectId: ids.alpha,
      sequenceId: ids.alphaSequence,
      baseProjectRevision: "8",
      outputDigest: `sha256:${"b".repeat(64)}`,
      preconditionCount: 4,
      policy: { id: "paper-edit-rough-cut-v1", overwrite: "forbidden" },
      items: [
        {
          ordinal: 1,
          sourceExcerptId: ids.alphaExcerpt,
          audio: {
            trackId: ids.audio,
            sourceStreamId: ids.sourceAudioStream,
            clipLocal: "rough_018f0a607b807a018000000000000801_audio_001",
          },
          alignmentLocal: "rough_018f0a607b807a018000000000000801_alignment_001",
        },
      ],
    });
    await expect(
      port.apply({ ...review }, { requestId: "ui:rough-cut:forged", intent: "Apply reviewed rough cut" }),
    ).rejects.toThrow("not owned by this Contracts session");

    const applyInput = { requestId: "ui:rough-cut:apply-1", intent: "Apply reviewed rough cut" };
    await expect(port.apply(review, applyInput)).rejects.toMatchObject({ code: "failed", status: 503 });
    const committed = await port.apply(review, applyInput);

    expect(applyBodies).toHaveLength(2);
    expect(applyBodies[1]).toBe(applyBodies[0]);
    expect(JSON.parse(applyBodies[0] ?? "{}")).toEqual({
      requestId: applyInput.requestId,
      intent: applyInput.intent,
      baseProjectRevision: "8",
      preconditions: roughCutPreview().preconditions,
      operations: [roughCutPreview().operation],
    });
    expect(committed).toMatchObject({
      transactionId: ids.transaction,
      committedProjectRevision: "9",
      allocation: [
        { local: "rough_018f0a607b807a018000000000000801_audio_001", kind: "clip", id: ids.clip },
        { local: "rough_018f0a607b807a018000000000000801_alignment_001", kind: "alignment", id: ids.alignment },
      ],
    });
  });

  it("rejects a preview envelope that does not close over the exact request", async () => {
    const preview = roughCutPreview();
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse({
          ...preview,
          operation: { ...preview.operation, roughCutLocalPrefix: "forged_preview" },
        }),
      ),
    );

    await expect(
      createContracts().editing.roughCut.preview({
        projectId: durableID(ids.alpha),
        sequenceId: durableID(ids.alphaSequence),
        timelineStart: time(0, 1),
        items: [
          {
            sourceExcerptId: durableID(ids.alphaExcerpt),
            sourceExcerptRevision: revisionString("3"),
            audio: {
              trackId: durableID(ids.audio),
              trackRevision: revisionString("4"),
              sourceStreamId: durableID(ids.sourceAudioStream),
            },
          },
        ],
      }),
    ).rejects.toThrow("does not match its request");
  });
});

function roughCutPreview() {
  const local = "rough_018f0a607b807a018000000000000801";
  return {
    baseProjectRevision: "8",
    preconditions: [
      { kind: "asset", id: ids.asset, revision: "2" },
      { kind: "narrative-node", id: ids.alphaExcerpt, revision: "3" },
      { kind: "sequence", id: ids.alphaSequence, revision: "5" },
      { kind: "track", id: ids.audio, revision: "4" },
    ],
    operation: {
      type: "derive-rough-cut",
      roughCutPolicy: {
        id: "paper-edit-rough-cut-v1",
        ordering: "request-order",
        interExcerptGap: { value: "0", scale: 1 },
        sourceHandles: "zero",
        rate: "1:1",
        overwrite: "forbidden",
        avGrouping: "one-link-group-per-two-lane-excerpt",
      },
      roughCutTimelineStart: { value: "0", scale: 1 },
      roughCutLocalPrefix: local,
      roughCutItems: [
        {
          sourceExcerptId: ids.alphaExcerpt,
          audio: { trackId: ids.audio, sourceStreamId: ids.sourceAudioStream },
        },
      ],
      derivedRoughCut: [
        {
          sourceExcerptId: ids.alphaExcerpt,
          sourceRange: range(1, 2, 3, 2),
          timelineRange: range(0, 1, 3, 2),
          audio: {
            clipAs: `${local}_audio_001`,
            trackId: ids.audio,
            sourceStreamId: ids.sourceAudioStream,
          },
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
  const local = "rough_018f0a607b807a018000000000000801";
  return {
    proposal: {
      id: ids.proposal,
      projectId: ids.alpha,
      sequenceId: ids.alphaSequence,
      requestId: "ui:rough-cut:apply-1",
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
      projectId: ids.alpha,
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
