import { commitCreatorEdit, undoCreatorEditTransaction } from "@open-cut/openapi/editing";

import { asRecord, canonicalLanguage, normalizeTimeRange, type TimeRange } from "./editing-exact.js";
import {
  type CursorString,
  cursorString,
  type DigestString,
  type DurableID,
  digestString,
  durableID,
  type RevisionString,
  revisionString,
} from "./exact.js";

export type CreatorEditPrecondition = Readonly<{
  kind: "narrative-node" | "transcript-correction";
  id: DurableID;
  revision: RevisionString;
}>;

export type CreatorNarrativeOperation =
  | Readonly<{
      type: "insert-section";
      createAs: string;
      parentId: DurableID;
      afterNodeId?: DurableID;
      title: string;
      language: string;
    }>
  | Readonly<{
      type: "update-section";
      nodeId: DurableID;
      title: string;
      language: string;
    }>
  | Readonly<{
      type: "insert-authored-text";
      createAs: string;
      parentId: DurableID;
      afterNodeId?: DurableID;
      purpose: "spoken" | "on-screen";
      language: string;
      text: string;
    }>
  | Readonly<{
      type: "update-authored-text";
      nodeId: DurableID;
      purpose: "spoken" | "on-screen";
      language: string;
      text: string;
    }>
  | Readonly<{
      type: "insert-source-excerpt";
      createAs: string;
      parentId: DurableID;
      afterNodeId?: DurableID;
      assetId: DurableID;
      acceptedFingerprint: DigestString;
      transcriptArtifactId: DurableID;
      transcriptSegmentIds: readonly DurableID[];
      sourceRange: TimeRange;
      language: string;
      correctionRevisions: readonly Readonly<{ id: DurableID; revision: RevisionString }>[];
    }>
  | Readonly<{
      type: "move-narrative-node";
      nodeId: DurableID;
      parentId: DurableID;
      afterNodeId?: DurableID;
    }>
  | Readonly<{
      type: "remove-narrative-node";
      nodeId: DurableID;
    }>;

export type CommitCreatorEditInput = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  requestId: string;
  intent: string;
  baseProjectRevision: RevisionString;
  preconditions: readonly CreatorEditPrecondition[];
  operations: readonly CreatorNarrativeOperation[];
}>;

export type UndoCreatorEditInput = Readonly<{
  projectId: DurableID;
  sequenceId: DurableID;
  transactionId: DurableID;
  requestId: string;
  intent?: string;
}>;

export type CreatorEditAllocation = Readonly<{
  local: string;
  kind: "narrative-node" | "caption" | "clip" | "alignment" | "link-group";
  id: DurableID;
}>;

export type CreatorEditCommit = Readonly<{
  proposalId: DurableID;
  transactionId: DurableID;
  committedProjectRevision: RevisionString;
  activityCursor: CursorString;
  changes: readonly Readonly<{ kind: string; id: DurableID; revision: RevisionString; tombstoned: boolean }>[];
  allocation: readonly CreatorEditAllocation[];
  undoesTransactionId?: DurableID;
  replayed: boolean;
}>;

export type CreatorEditFailureCode = "conflict" | "not-found" | "invalid" | "denied" | "failed";

export class CreatorEditError extends Error {
  readonly code: CreatorEditFailureCode;
  readonly status: number;

  constructor(code: CreatorEditFailureCode, status: number) {
    super(`Creator edit failed: ${code}`);
    this.name = "CreatorEditError";
    this.code = code;
    this.status = status;
  }
}

export interface EditWritePort {
  commit(input: CommitCreatorEditInput, signal?: AbortSignal): Promise<CreatorEditCommit>;
  undo(input: UndoCreatorEditInput, signal?: AbortSignal): Promise<CreatorEditCommit>;
}

export type CreatorWireEditBody = Parameters<typeof commitCreatorEdit>[2];

export function createEditWritePort(): EditWritePort {
  return {
    commit: async (input, signal) => {
      const normalized = normalizeCreatorEditInput(input);
      return commitCreatorWireEdit(input.projectId, input.sequenceId, normalized, signal);
    },
    undo: async (input, signal) => {
      validateCreatorRequestID(input.requestId);
      if (input.intent !== undefined) validateCreatorIntent(input.intent, true);
      const response = await undoCreatorEditTransaction(
        input.projectId,
        input.sequenceId,
        input.transactionId,
        { requestId: input.requestId, ...(input.intent === undefined ? {} : { intent: input.intent }) },
        { signal },
      );
      if (response.status !== 200) throw creatorEditResponseError(response.status);
      return normalizeCreatorEditCommit(response.data, input.projectId, input.sequenceId);
    },
  };
}

export async function commitCreatorWireEdit(
  projectId: DurableID,
  sequenceId: DurableID,
  body: CreatorWireEditBody,
  signal?: AbortSignal,
): Promise<CreatorEditCommit> {
  const response = await commitCreatorEdit(projectId, sequenceId, body, { signal });
  if (response.status !== 200) throw creatorEditResponseError(response.status);
  return normalizeCreatorEditCommit(response.data, projectId, sequenceId);
}

function normalizeCreatorEditInput(input: CommitCreatorEditInput) {
  validateCreatorRequestID(input.requestId);
  validateCreatorIntent(input.intent, false);
  const baseProjectRevision = revisionString(input.baseProjectRevision);
  if (input.operations.length < 1 || input.operations.length > 512 || input.preconditions.length > 2048) {
    throw new Error("Creator edit exceeds its operation budget");
  }
  return {
    requestId: input.requestId,
    intent: input.intent,
    baseProjectRevision,
    preconditions: input.preconditions.map((condition) => ({
      kind: condition.kind,
      id: durableID(condition.id),
      revision: revisionString(condition.revision),
    })),
    operations: input.operations.map((operation) => {
      if (operation.type === "move-narrative-node") {
        return {
          type: operation.type,
          nodeId: durableID(operation.nodeId),
          parentId: durableID(operation.parentId),
          ...(operation.afterNodeId === undefined ? {} : { after: { id: durableID(operation.afterNodeId) } }),
        };
      }
      if (operation.type === "remove-narrative-node") {
        return { type: operation.type, nodeId: durableID(operation.nodeId) };
      }
      if (operation.type === "insert-section" || operation.type === "update-section") {
        const language = canonicalLanguage(operation.language, "Narrative section");
        validateCreatorText(operation.title, "Narrative section title");
        if (operation.type === "insert-section") {
          validateLocalIdentity(operation.createAs);
          return {
            type: operation.type,
            createAs: operation.createAs,
            parentId: durableID(operation.parentId),
            ...(operation.afterNodeId === undefined ? {} : { after: { id: durableID(operation.afterNodeId) } }),
            title: operation.title,
            language,
          };
        }
        return {
          type: operation.type,
          nodeId: durableID(operation.nodeId),
          title: operation.title,
          language,
        };
      }
      if (operation.type === "insert-source-excerpt") {
        validateLocalIdentity(operation.createAs);
        const segmentIds = validateDistinctIDs(operation.transcriptSegmentIds, "Transcript segment", 256);
        const correctionIds = new Set<string>();
        const correctionRevisions = operation.correctionRevisions.map((correction) => {
          const id = durableID(correction.id);
          if (correctionIds.has(id)) throw new Error("Transcript correction identity is duplicated");
          correctionIds.add(id);
          return { correction: { id }, revision: revisionString(correction.revision) };
        });
        if (correctionRevisions.length > 256) throw new Error("Transcript correction selection exceeds its budget");
        return {
          type: operation.type,
          createAs: operation.createAs,
          parentId: durableID(operation.parentId),
          ...(operation.afterNodeId === undefined ? {} : { after: { id: durableID(operation.afterNodeId) } }),
          assetId: durableID(operation.assetId),
          acceptedFingerprint: digestString(operation.acceptedFingerprint),
          transcriptArtifactId: durableID(operation.transcriptArtifactId),
          transcriptSegmentIds: segmentIds,
          sourceRange: normalizeTimeRange(operation.sourceRange),
          language: canonicalLanguage(operation.language, "SourceExcerpt"),
          correctionRevisions,
        };
      }
      const language = canonicalLanguage(operation.language, "authored text");
      validateCreatorText(operation.text, "authored text");
      if (operation.type === "insert-authored-text") {
        validateLocalIdentity(operation.createAs);
        return {
          type: operation.type,
          createAs: operation.createAs,
          parentId: durableID(operation.parentId),
          ...(operation.afterNodeId === undefined ? {} : { after: { id: durableID(operation.afterNodeId) } }),
          authoredTextPurpose: operation.purpose,
          language,
          text: operation.text,
        };
      }
      return {
        type: operation.type,
        nodeId: durableID(operation.nodeId),
        authoredTextPurpose: operation.purpose,
        language,
        text: operation.text,
      };
    }),
  };
}

function validateLocalIdentity(value: string): void {
  if (!/^[a-z][a-z0-9_-]{0,63}$/.test(value)) throw new Error("Creator edit local identity is invalid");
}

function validateCreatorText(value: string, label: string): void {
  const bytes = new TextEncoder().encode(value).length;
  if (bytes < 1 || bytes > 262_144) throw new Error(`${label} exceeds its text budget`);
}

function validateDistinctIDs(values: readonly DurableID[], label: string, maximum: number): DurableID[] {
  if (values.length < 1 || values.length > maximum) throw new Error(`${label} selection exceeds its budget`);
  const seen = new Set<string>();
  return values.map((value) => {
    const id = durableID(value);
    if (seen.has(id)) throw new Error(`${label} identity is duplicated`);
    seen.add(id);
    return id;
  });
}

export function normalizeCreatorEditCommit(
  value: unknown,
  expectedProjectId: DurableID,
  expectedSequenceId: DurableID,
): CreatorEditCommit {
  const receipt = asRecord(value);
  const proposal = asRecord(receipt.proposal);
  const transaction = asRecord(receipt.transaction);
  const actor = asRecord(proposal.actor);
  const transactionActor = asRecord(transaction.actor);
  if (receipt.replayed !== true && receipt.replayed !== false) {
    throw new Error("Creator edit receipt is invalid");
  }
  if (
    actor.kind !== "creator" ||
    transactionActor.kind !== "creator" ||
    proposal.runId !== undefined ||
    proposal.turnId !== undefined ||
    proposal.status !== "applied" ||
    proposal.projectId !== expectedProjectId ||
    proposal.sequenceId !== expectedSequenceId ||
    transaction.projectId !== expectedProjectId ||
    transaction.proposalId !== proposal.id ||
    proposal.appliedTransactionId !== transaction.id ||
    !Array.isArray(proposal.allocation) ||
    !Array.isArray(transaction.changes)
  ) {
    throw new Error("Creator edit receipt crossed its authority or scope");
  }
  const allocation = proposal.allocation.map((value) => {
    const item = asRecord(value);
    if (
      typeof item.local !== "string" ||
      (item.kind !== "narrative-node" &&
        item.kind !== "caption" &&
        item.kind !== "clip" &&
        item.kind !== "alignment" &&
        item.kind !== "link-group")
    ) {
      throw new Error("Creator edit allocation is invalid");
    }
    return { local: item.local, kind: item.kind, id: durableID(item.id) } as const;
  });
  const changes = transaction.changes.map((value) => {
    const change = asRecord(value);
    if (typeof change.kind !== "string" || change.kind.length === 0) {
      throw new Error("Creator edit change is invalid");
    }
    return {
      kind: change.kind,
      id: durableID(change.id),
      revision: revisionString(change.after),
      tombstoned: change.tombstoned === true,
    };
  });
  return {
    proposalId: durableID(proposal.id),
    transactionId: durableID(transaction.id),
    committedProjectRevision: revisionString(transaction.committedProjectRevision),
    activityCursor: cursorString(receipt.activityCursor),
    changes,
    allocation,
    ...(transaction.undoesTransactionId === undefined
      ? {}
      : { undoesTransactionId: durableID(transaction.undoesTransactionId) }),
    replayed: receipt.replayed,
  };
}

export function creatorEditResponseError(status: number): CreatorEditError {
  if (status === 409) return new CreatorEditError("conflict", status);
  if (status === 404) return new CreatorEditError("not-found", status);
  if (status === 422 || status === 400) return new CreatorEditError("invalid", status);
  if (status === 401 || status === 403) return new CreatorEditError("denied", status);
  return new CreatorEditError("failed", status);
}

export function validateCreatorRequestID(value: string): void {
  if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(value)) {
    throw new Error("Creator edit request identity is invalid");
  }
}

export function validateCreatorIntent(value: string, allowsEmpty: boolean): void {
  const bytes = new TextEncoder().encode(value).length;
  if (bytes > 4000 || (!allowsEmpty && bytes === 0)) throw new Error("Creator edit intent is invalid");
}
