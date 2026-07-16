import { listCreatorEditTransactions } from "@open-cut/openapi/creator";

import { creatorEditResponseError } from "./creator-editing.js";
import { asRecord } from "./editing-exact.js";
import {
  type CursorString,
  cursorString,
  type DurableID,
  durableID,
  type RevisionString,
  revisionString,
} from "./exact.js";

export type CreatorHistoryChange = Readonly<{
  kind:
    | "narrative-document"
    | "narrative-node"
    | "sequence"
    | "track"
    | "caption"
    | "alignment"
    | "clip"
    | "link-group"
    | "asset"
    | "transcript-correction";
  id: DurableID;
  beforeRevision?: RevisionString;
  revision: RevisionString;
  tombstoned: boolean;
}>;

export type CreatorHistoryTransaction = Readonly<{
  id: DurableID;
  intent: string;
  actor: "creator" | "agent";
  committedProjectRevision: RevisionString;
  changes: readonly CreatorHistoryChange[];
  undoesTransactionId?: DurableID;
  committedAt: string;
}>;

export type CreatorHistoryPage = Readonly<{
  transactions: readonly CreatorHistoryTransaction[];
  nextBefore?: RevisionString;
  activityCursor: CursorString;
}>;

export type ListCreatorHistoryInput = Readonly<{
  projectId: DurableID;
  before?: RevisionString;
  limit?: number;
}>;

export interface CreatorHistoryPort {
  list(input: ListCreatorHistoryInput, signal?: AbortSignal): Promise<CreatorHistoryPage>;
}

export function createCreatorHistoryPort(): CreatorHistoryPort {
  return {
    list: async (input, signal) => {
      const projectId = durableID(input.projectId);
      const before = input.before === undefined ? undefined : revisionString(input.before);
      if (input.limit !== undefined && (!Number.isInteger(input.limit) || input.limit < 1 || input.limit > 50)) {
        throw new Error("Creator history limit is invalid");
      }
      const response = await listCreatorEditTransactions(
        projectId,
        {
          ...(before === undefined ? {} : { before }),
          ...(input.limit === undefined ? {} : { limit: input.limit }),
        },
        { signal },
      );
      if (response.status !== 200) throw creatorEditResponseError(response.status);
      return normalizeCreatorHistory(response.data, before);
    },
  };
}

function normalizeCreatorHistory(value: unknown, before: RevisionString | undefined): CreatorHistoryPage {
  const page = asRecord(value);
  if (!Array.isArray(page.transactions) || page.transactions.length > 50) {
    throw new Error("Creator history transactions are invalid");
  }
  const seen = new Set<string>();
  let previousRevision = before;
  const transactions = page.transactions.map((entry) => {
    const transaction = asRecord(entry);
    const id = durableID(transaction.id);
    const revision = revisionString(transaction.committedProjectRevision);
    if (seen.has(id) || (previousRevision !== undefined && BigInt(revision) >= BigInt(previousRevision))) {
      throw new Error("Creator history order is invalid");
    }
    seen.add(id);
    previousRevision = revision;
    if (
      typeof transaction.intent !== "string" ||
      transaction.intent.length < 1 ||
      transaction.intent.length > 262_144
    ) {
      throw new Error("Creator history intent is invalid");
    }
    const actor = transaction.actor;
    if (actor !== "creator" && actor !== "agent") {
      throw new Error("Creator history actor is invalid");
    }
    if (!Array.isArray(transaction.changes) || transaction.changes.length > 2048) {
      throw new Error("Creator history changes are invalid");
    }
    const committedAt = timestamp(transaction.committedAt);
    const normalized: CreatorHistoryTransaction = {
      id,
      intent: transaction.intent,
      actor,
      committedProjectRevision: revision,
      changes: transaction.changes.map(normalizeChange),
      ...(transaction.undoesTransactionId === undefined
        ? {}
        : { undoesTransactionId: durableID(transaction.undoesTransactionId) }),
      committedAt,
    };
    return normalized;
  });
  const nextBefore = page.nextBefore === undefined ? undefined : revisionString(page.nextBefore);
  if (
    nextBefore !== undefined &&
    (transactions.length === 0 || nextBefore !== transactions.at(-1)?.committedProjectRevision)
  ) {
    throw new Error("Creator history continuation is invalid");
  }
  return {
    transactions,
    ...(nextBefore === undefined ? {} : { nextBefore }),
    activityCursor: cursorString(page.activityCursor),
  };
}

function normalizeChange(value: unknown): CreatorHistoryChange {
  const change = asRecord(value);
  const kind = historyKind(change.kind);
  return {
    kind,
    id: durableID(change.id),
    ...(change.before === undefined ? {} : { beforeRevision: revisionString(change.before) }),
    revision: revisionString(change.after),
    tombstoned: change.tombstoned === true,
  };
}

function historyKind(value: unknown): CreatorHistoryChange["kind"] {
  if (
    value !== "narrative-document" &&
    value !== "narrative-node" &&
    value !== "sequence" &&
    value !== "track" &&
    value !== "caption" &&
    value !== "alignment" &&
    value !== "clip" &&
    value !== "link-group" &&
    value !== "asset" &&
    value !== "transcript-correction"
  ) {
    throw new Error("Creator history change kind is invalid");
  }
  return value;
}

function timestamp(value: unknown): string {
  if (
    typeof value !== "string" ||
    value.length > 64 ||
    !/^\d{4}-\d{2}-\d{2}T/.test(value) ||
    Number.isNaN(Date.parse(value))
  ) {
    throw new Error("Creator history timestamp is invalid");
  }
  return value;
}
