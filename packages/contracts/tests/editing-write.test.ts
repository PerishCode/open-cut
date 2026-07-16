import { afterEach, describe, expect, it, vi } from "vitest";
import { createContracts, digestString, durableID, int64String, revisionString } from "../src/index.js";

import { jsonResponse } from "./http-fixtures.js";
import { testIDs as ids } from "./test-identities.js";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("Creator edit write contracts", () => {
  it("maps authored text into one creator commit and validates its durable receipt", async () => {
    const bodies: unknown[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
        expect(String(input)).toBe(`/api/v1/projects/${ids.alpha}/sequences/${ids.alphaSequence}/edits`);
        bodies.push(JSON.parse(String(init?.body)) as unknown);
        return jsonResponse(commitReceipt());
      }),
    );

    const result = await createContracts().editing.write.commit({
      projectId: durableID(ids.alpha),
      sequenceId: durableID(ids.alphaSequence),
      requestId: "ui:creator-edit:commit-1",
      intent: "Draft the opening",
      baseProjectRevision: revisionString("1"),
      preconditions: [{ kind: "narrative-node", id: durableID(ids.alphaRoot), revision: revisionString("1") }],
      operations: [
        {
          type: "insert-authored-text",
          createAs: "opening",
          parentId: durableID(ids.alphaRoot),
          purpose: "spoken",
          language: "und",
          text: "Start with the human problem.",
        },
      ],
    });

    expect(bodies).toEqual([
      {
        requestId: "ui:creator-edit:commit-1",
        intent: "Draft the opening",
        baseProjectRevision: "1",
        preconditions: [{ kind: "narrative-node", id: ids.alphaRoot, revision: "1" }],
        operations: [
          {
            type: "insert-authored-text",
            createAs: "opening",
            parentId: ids.alphaRoot,
            authoredTextPurpose: "spoken",
            language: "und",
            text: "Start with the human problem.",
          },
        ],
      },
    ]);
    expect(result).toMatchObject({
      proposalId: ids.proposal,
      transactionId: ids.transaction,
      committedProjectRevision: "2",
      allocation: [{ local: "opening", kind: "narrative-node", id: ids.alphaText }],
      replayed: false,
    });
  });

  it("maps Narrative move and remove without exposing wire reference syntax", async () => {
    const bodies: unknown[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        bodies.push(JSON.parse(String(init?.body)) as unknown);
        return jsonResponse(commitReceipt());
      }),
    );

    await createContracts().editing.write.commit({
      projectId: durableID(ids.alpha),
      sequenceId: durableID(ids.alphaSequence),
      requestId: "ui:creator-edit:structure-1",
      intent: "Reorder and remove Narrative paragraphs",
      baseProjectRevision: revisionString("4"),
      preconditions: [
        { kind: "narrative-node", id: durableID(ids.alphaRoot), revision: revisionString("3") },
        { kind: "narrative-node", id: durableID(ids.alphaText), revision: revisionString("2") },
      ],
      operations: [
        {
          type: "move-narrative-node",
          nodeId: durableID(ids.alphaText),
          parentId: durableID(ids.alphaRoot),
          afterNodeId: durableID(ids.alphaSequence),
        },
        { type: "remove-narrative-node", nodeId: durableID(ids.alphaText) },
      ],
    });

    expect(bodies[0]).toMatchObject({
      operations: [
        {
          type: "move-narrative-node",
          nodeId: ids.alphaText,
          parentId: ids.alphaRoot,
          after: { id: ids.alphaSequence },
        },
        { type: "remove-narrative-node", nodeId: ids.alphaText },
      ],
    });
  });

  it("maps explicit Section creation and complete title replacement", async () => {
    const bodies: unknown[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        bodies.push(JSON.parse(String(init?.body)) as unknown);
        return jsonResponse(commitReceipt());
      }),
    );

    await createContracts().editing.write.commit({
      projectId: durableID(ids.alpha),
      sequenceId: durableID(ids.alphaSequence),
      requestId: "ui:creator-edit:section-1",
      intent: "Create and title explicit Narrative sections",
      baseProjectRevision: revisionString("5"),
      preconditions: [
        { kind: "narrative-node", id: durableID(ids.alphaRoot), revision: revisionString("4") },
        { kind: "narrative-node", id: durableID(ids.alphaText), revision: revisionString("2") },
      ],
      operations: [
        {
          type: "insert-section",
          createAs: "problem",
          parentId: durableID(ids.alphaRoot),
          afterNodeId: durableID(ids.alphaText),
          title: "The human problem",
          language: "en-US",
        },
        {
          type: "update-section",
          nodeId: durableID(ids.alphaText),
          title: "Why this matters",
          language: "zh-Hans",
        },
      ],
    });

    expect(bodies[0]).toMatchObject({
      operations: [
        {
          type: "insert-section",
          createAs: "problem",
          parentId: ids.alphaRoot,
          after: { id: ids.alphaText },
          title: "The human problem",
          language: "en-US",
        },
        {
          type: "update-section",
          nodeId: ids.alphaText,
          title: "Why this matters",
          language: "zh-Hans",
        },
      ],
    });
  });

  it("maps immutable SourceExcerpt evidence and exact correction preconditions", async () => {
    const bodies: unknown[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        bodies.push(JSON.parse(String(init?.body)) as unknown);
        return jsonResponse(commitReceipt());
      }),
    );
    const fingerprint = digestString(`sha256:${"1".repeat(64)}`);

    await createContracts().editing.write.commit({
      projectId: durableID(ids.alpha),
      sequenceId: durableID(ids.alphaSequence),
      requestId: "ui:creator-edit:excerpt-1",
      intent: "Insert exact transcript evidence",
      baseProjectRevision: revisionString("6"),
      preconditions: [
        { kind: "narrative-node", id: durableID(ids.alphaRoot), revision: revisionString("5") },
        {
          kind: "transcript-correction",
          id: durableID(ids.alphaTranscriptCorrection),
          revision: revisionString("3"),
        },
      ],
      operations: [
        {
          type: "insert-source-excerpt",
          createAs: "evidence",
          parentId: durableID(ids.alphaRoot),
          afterNodeId: durableID(ids.alphaText),
          assetId: durableID(ids.asset),
          acceptedFingerprint: fingerprint,
          transcriptArtifactId: durableID(ids.alphaTranscriptArtifact),
          transcriptSegmentIds: [durableID(ids.alphaTranscriptSegment)],
          sourceRange: {
            start: { value: int64String("1"), scale: 2 },
            duration: { value: int64String("3"), scale: 2 },
          },
          language: "en-US",
          correctionRevisions: [{ id: durableID(ids.alphaTranscriptCorrection), revision: revisionString("3") }],
        },
      ],
    });

    expect(bodies[0]).toMatchObject({
      preconditions: [
        { kind: "narrative-node", id: ids.alphaRoot, revision: "5" },
        { kind: "transcript-correction", id: ids.alphaTranscriptCorrection, revision: "3" },
      ],
      operations: [
        {
          type: "insert-source-excerpt",
          parentId: ids.alphaRoot,
          after: { id: ids.alphaText },
          assetId: ids.asset,
          acceptedFingerprint: fingerprint,
          transcriptArtifactId: ids.alphaTranscriptArtifact,
          transcriptSegmentIds: [ids.alphaTranscriptSegment],
          sourceRange: { start: { value: "1", scale: 2 }, duration: { value: "3", scale: 2 } },
          language: "en-US",
          correctionRevisions: [{ correction: { id: ids.alphaTranscriptCorrection }, revision: "3" }],
        },
      ],
    });
  });

  it("surfaces conflict as a typed Creator failure and keeps undo creator-only", async () => {
    const calls: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request) => {
        const url = String(input);
        calls.push(url);
        if (url.endsWith("/undo")) return jsonResponse(commitReceipt(ids.undoTransaction));
        return new Response(JSON.stringify({ title: "Conflict", status: 409 }), { status: 409 });
      }),
    );
    const write = createContracts().editing.write;
    await expect(
      write.commit({
        projectId: durableID(ids.alpha),
        sequenceId: durableID(ids.alphaSequence),
        requestId: "ui:creator-edit:conflict",
        intent: "Update paragraph",
        baseProjectRevision: revisionString("1"),
        preconditions: [{ kind: "narrative-node", id: durableID(ids.alphaText), revision: revisionString("1") }],
        operations: [
          {
            type: "update-authored-text",
            nodeId: durableID(ids.alphaText),
            purpose: "spoken",
            language: "und",
            text: "A newer draft.",
          },
        ],
      }),
    ).rejects.toMatchObject({ code: "conflict", status: 409 });

    const undone = await write.undo({
      projectId: durableID(ids.alpha),
      sequenceId: durableID(ids.alphaSequence),
      transactionId: durableID(ids.transaction),
      requestId: "ui:creator-edit:undo-1",
    });
    expect(calls[1]).toBe(
      `/api/v1/projects/${ids.alpha}/sequences/${ids.alphaSequence}/transactions/${ids.transaction}/undo`,
    );
    expect(undone.transactionId).toBe(ids.undoTransaction);
  });
});

function commitReceipt(transactionId: string = ids.transaction) {
  return {
    proposal: {
      id: ids.proposal,
      projectId: ids.alpha,
      sequenceId: ids.alphaSequence,
      requestId: "ui:creator-edit:commit-1",
      actor: { kind: "creator", creatorId: ids.creator },
      status: "applied",
      appliedTransactionId: transactionId,
      allocation: [{ local: "opening", kind: "narrative-node", id: ids.alphaText }],
    },
    transaction: {
      id: transactionId,
      proposalId: ids.proposal,
      projectId: ids.alpha,
      actor: { kind: "creator", creatorId: ids.creator },
      committedProjectRevision: "2",
      changes: [{ kind: "narrative-node", id: ids.alphaText, after: "1" }],
    },
    activityCursor: "7",
    replayed: false,
  };
}
