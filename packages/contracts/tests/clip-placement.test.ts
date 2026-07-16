import { afterEach, describe, expect, it, vi } from "vitest";

import { createContracts, digestString, durableID, int64String, revisionString } from "../src/index.js";
import { jsonResponse } from "./http-fixtures.js";
import { testIDs as ids } from "./test-identities.js";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("Creator Clip placement Contracts", () => {
  it("hides exact linked A/V operations and byte-replays one direct Creator apply", async () => {
    const previewBodies: Record<string, unknown>[] = [];
    const applyBodies: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/clip-placement-preview")) {
          const body = JSON.parse(String(init?.body)) as Record<string, unknown>;
          previewBodies.push(body);
          return jsonResponse(placementPreview(String(body.localPrefix)));
        }
        if (url.endsWith("/edits")) {
          applyBodies.push(String(init?.body));
          if (applyBodies.length === 1) {
            return new Response(JSON.stringify({ title: "Unavailable", status: 503 }), {
              status: 503,
              headers: { "content-type": "application/problem+json" },
            });
          }
          return jsonResponse(commitReceipt(String(previewBodies[0]?.localPrefix)));
        }
        throw new Error(`unexpected request ${url}`);
      }),
    );

    const port = createContracts().editing.clipPlacement;
    const review = await port.preview(placementInput());
    const request = previewBodies[0];
    expect(request?.localPrefix).toMatch(/^place_[a-z0-9]{32}$/);
    expect(request).toEqual({
      assetId: ids.asset,
      assetRevision: "3",
      acceptedFingerprint: `sha256:${"a".repeat(64)}`,
      sourceRange: range(-2, 1, 1, 1),
      timelineStart: time(4, 1),
      localPrefix: request?.localPrefix,
      video: { trackId: ids.video, trackRevision: "5", sourceStreamId: ids.sourceVideoStream },
      audio: { trackId: ids.audio, trackRevision: "6", sourceStreamId: ids.sourceAudioStream },
    });
    expect(review).toEqual({
      projectId: ids.alpha,
      sequenceId: ids.alphaSequence,
      baseProjectRevision: "8",
      activityCursor: "12",
      outputDigest: `sha256:${"d".repeat(64)}`,
      assetId: ids.asset,
      assetRevision: "3",
      acceptedFingerprint: `sha256:${"a".repeat(64)}`,
      sourceRange: range(-2, 1, 1, 1),
      timelineRange: range(4, 1, 1, 1),
      lanes: [
        { type: "video", trackId: ids.video, sourceStreamId: ids.sourceVideoStream },
        { type: "audio", trackId: ids.audio, sourceStreamId: ids.sourceAudioStream },
      ],
      linked: true,
      preconditionCount: 4,
    });
    expect(JSON.stringify(review)).not.toContain(String(request?.localPrefix));
    await expect(
      port.apply({ ...review }, { requestId: "ui:placement:forged", intent: "Place selected source" }),
    ).rejects.toThrow("not owned by this Contracts session");

    const applyInput = { requestId: "ui:placement:apply-1", intent: "Place selected source range" };
    await expect(port.apply(review, applyInput)).rejects.toMatchObject({ code: "failed", status: 503 });
    const committed = await port.apply(review, applyInput);
    expect(applyBodies).toHaveLength(2);
    expect(applyBodies[1]).toBe(applyBodies[0]);
    expect(JSON.parse(applyBodies[0] ?? "{}")).toEqual({
      requestId: applyInput.requestId,
      intent: applyInput.intent,
      baseProjectRevision: "8",
      preconditions: placementPreview(String(request?.localPrefix)).preconditions,
      operations: placementPreview(String(request?.localPrefix)).operations,
    });
    expect(committed).toMatchObject({
      transactionId: ids.transaction,
      committedProjectRevision: "9",
      changes: [
        { kind: "link-group", id: ids.linkGroup, revision: "1" },
        { kind: "clip", id: ids.clip, revision: "1" },
        { kind: "clip", id: ids.clipSecond, revision: "1" },
      ],
    });
  });

  it("rejects a forged LinkGroup closure and lane-less placement before transport", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        const body = JSON.parse(String(init?.body)) as Record<string, unknown>;
        const preview = placementPreview(String(body.localPrefix));
        return jsonResponse({
          ...preview,
          operations: [
            preview.operations[0],
            { ...preview.operations[1], linkGroup: { local: `${String(body.localPrefix)}_forged` } },
          ],
        });
      }),
    );
    await expect(createContracts().editing.clipPlacement.preview(placementInput())).rejects.toThrow(
      "LinkGroup reference is invalid",
    );

    const transport = vi.fn();
    vi.stubGlobal("fetch", transport);
    const input = placementInput();
    await expect(
      createContracts().editing.clipPlacement.preview({ ...input, video: undefined, audio: undefined }),
    ).rejects.toThrow("no selected lane");
    expect(transport).not.toHaveBeenCalled();
  });
});

function placementInput() {
  return {
    projectId: durableID(ids.alpha),
    sequenceId: durableID(ids.alphaSequence),
    assetId: durableID(ids.asset),
    assetRevision: revisionString("3"),
    acceptedFingerprint: digestString(`sha256:${"a".repeat(64)}`),
    sourceRange: range(-2, 1, 1, 1),
    timelineStart: time(4, 1),
    video: {
      trackId: durableID(ids.video),
      trackRevision: revisionString("5"),
      sourceStreamId: durableID(ids.sourceVideoStream),
    },
    audio: {
      trackId: durableID(ids.audio),
      trackRevision: revisionString("6"),
      sourceStreamId: durableID(ids.sourceAudioStream),
    },
  };
}

function placementPreview(localPrefix: string) {
  const sourceRange = range(-2, 1, 1, 1);
  const timelineRange = range(4, 1, 1, 1);
  const groupLocal = `${localPrefix}_group`;
  return {
    baseProjectRevision: "8",
    preconditions: [
      { kind: "asset", id: ids.asset, revision: "3" },
      { kind: "sequence", id: ids.alphaSequence, revision: "7" },
      { kind: "track", id: ids.audio, revision: "6" },
      { kind: "track", id: ids.video, revision: "5" },
    ],
    operations: [
      {
        type: "add-clip",
        createAs: `${localPrefix}_video`,
        trackId: ids.video,
        assetId: ids.asset,
        sourceStreamId: ids.sourceVideoStream,
        sourceRange,
        timelineRange,
        enabled: true,
        createLinkGroupAs: groupLocal,
      },
      {
        type: "add-clip",
        createAs: `${localPrefix}_audio`,
        trackId: ids.audio,
        assetId: ids.asset,
        sourceStreamId: ids.sourceAudioStream,
        sourceRange,
        timelineRange,
        enabled: true,
        linkGroup: { local: groupLocal },
      },
    ],
    assetId: ids.asset,
    assetRevision: "3",
    acceptedFingerprint: `sha256:${"a".repeat(64)}`,
    sourceRange,
    timelineRange,
    lanes: [
      { type: "video", trackId: ids.video, sourceStreamId: ids.sourceVideoStream },
      { type: "audio", trackId: ids.audio, sourceStreamId: ids.sourceAudioStream },
    ],
    linked: true,
    outputDigest: `sha256:${"d".repeat(64)}`,
    activityCursor: "12",
  };
}

function commitReceipt(localPrefix: string) {
  return {
    proposal: {
      id: ids.proposal,
      projectId: ids.alpha,
      sequenceId: ids.alphaSequence,
      requestId: "ui:placement:apply-1",
      actor: { kind: "creator", creatorId: ids.creator },
      status: "applied",
      appliedTransactionId: ids.transaction,
      allocation: [
        { local: `${localPrefix}_group`, kind: "link-group", id: ids.linkGroup },
        { local: `${localPrefix}_video`, kind: "clip", id: ids.clip },
        { local: `${localPrefix}_audio`, kind: "clip", id: ids.clipSecond },
      ],
    },
    transaction: {
      id: ids.transaction,
      proposalId: ids.proposal,
      projectId: ids.alpha,
      actor: { kind: "creator", creatorId: ids.creator },
      committedProjectRevision: "9",
      changes: [
        { kind: "link-group", id: ids.linkGroup, after: "1", tombstoned: false },
        { kind: "clip", id: ids.clip, after: "1", tombstoned: false },
        { kind: "clip", id: ids.clipSecond, after: "1", tombstoned: false },
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
