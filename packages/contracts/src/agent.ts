import {
  beginCreatorAgentRun,
  cancelCreatorAgentRun,
  continueCreatorAgentRun,
  getWatchCreatorAgentPresentationUrl,
  interruptCreatorAgentTurn,
  listCreatorAgentConversation,
  listCreatorAgentRuns,
  listCreatorAgentTurnReceipts,
  listCreatorAgentTurns,
  showCreatorAgentRun,
  showLocalAgentAvailability,
} from "@open-cut/openapi/agent";
import { normalizeRational, normalizeTimeRange, type TimeRange } from "./editing-exact.js";
import {
  type CursorString,
  cursorString,
  type DurableID,
  digestString,
  durableID,
  type RevisionString,
  revisionString,
} from "./exact.js";
import { asRecord, isBoundedInteger, responseError, timestamp } from "./media-validation.js";
import type { RationalTime } from "./projects.js";
import { readServerEvents } from "./sse.js";

const requestIDPattern = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/;
const encoder = new TextEncoder();

export type AgentRunStatus = "authorizing" | "active" | "waiting" | "paused" | "completed" | "failed" | "cancelled";
export type AgentTurnStatus = "starting" | "active" | "detached" | "completed" | "failed" | "cancelled" | "superseded";

type AgentEntityAttachmentKind = "asset" | "narrative-node" | "clip" | "caption" | "track";

export type AgentContextAttachment =
  | Readonly<{
      kind: AgentEntityAttachmentKind;
      entity: Readonly<{ id: DurableID; revision: RevisionString }>;
    }>
  | Readonly<{
      kind: "transcript-segment";
      transcript: Readonly<{ artifactId: DurableID; segmentId: DurableID }>;
    }>
  | Readonly<{
      kind: "sequence-point";
      point: Readonly<{ sequenceId: DurableID; revision: RevisionString; time: RationalTime }>;
    }>
  | Readonly<{
      kind: "sequence-range";
      range: Readonly<{ sequenceId: DurableID; revision: RevisionString; range: TimeRange }>;
    }>;

export type AgentAvailability = Readonly<{
  adapterId: "codex-cli-v1";
  promptVersion: "open-cut-agent-v1";
  state: "available" | "missing" | "unauthenticated" | "incompatible";
  version?: string;
}>;

export type AgentTurn = Readonly<{
  id: DurableID;
  generation: RevisionString;
  sequenceId?: DurableID;
  status: AgentTurnStatus;
  startedAt: string;
  endedAt?: string;
}>;

export type AgentRun = Readonly<{
  id: DurableID;
  projectId: DurableID;
  intent: string;
  agentId?: DurableID;
  status: AgentRunStatus;
  waitingReason?: string;
  currentTurn: AgentTurn;
  activityCursor: CursorString;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
}>;

export type AgentConversationMessage = Readonly<{
  id: DurableID;
  projectId: DurableID;
  runId: DurableID;
  turnId: DurableID;
  ordinal: CursorString;
  role: "creator" | "agent" | "notice";
  text: string;
  attachments: readonly AgentContextAttachment[];
  createdAt: string;
}>;

export type AgentConversationPage = Readonly<{
  projectId: DurableID;
  runId: DurableID;
  messages: readonly AgentConversationMessage[];
  nextAfter?: CursorString;
}>;

export type AgentPresentation = Readonly<{
  runId: DurableID;
  turnId: DurableID;
  sequence: CursorString;
  kind:
    | "turn-started"
    | "context-rebuilt"
    | "tool-started"
    | "tool-completed"
    | "message-completed"
    | "turn-completed"
    | "turn-failed";
  tool?: "command" | "file-change" | "reasoning" | "web-search" | "plan";
}>;

export type BeginAgentRunInput = Readonly<{
  requestId: string;
  message: string;
  sequenceId?: DurableID;
  attachments: readonly AgentContextAttachment[];
}>;

export type ContinueAgentRunInput = Readonly<{
  requestId: string;
  expectedGeneration: RevisionString;
  message: string;
  sequenceId?: DurableID;
  attachments: readonly AgentContextAttachment[];
}>;

export type CommandReceiptRef = Readonly<{
  kind: string;
  id: string;
  revision?: RevisionString;
}>;

export type CommandReceipt = Readonly<{
  schema: "open-cut/command-receipt/v2";
  id: DurableID;
  projectId: DurableID;
  runId: DurableID;
  turnId: DurableID;
  ordinal: CursorString;
  class: "evidence" | "outcome";
  command: string;
  commandFingerprint: import("./exact.js").DigestString;
  inputDigest: import("./exact.js").DigestString;
  requestId?: string;
  resultDigest: import("./exact.js").DigestString;
  status:
    | "succeeded"
    | "accepted"
    | "waiting"
    | "approval-required"
    | "conflict"
    | "not-found"
    | "unavailable"
    | "incompatible"
    | "invalid"
    | "failed";
  resultRefs: readonly CommandReceiptRef[];
  projectRevision?: RevisionString;
  activityCursor?: CursorString;
  createdAt: string;
}>;

export type TurnReceiptPage = Readonly<{
  projectId: DurableID;
  runId: DurableID;
  turnId: DurableID;
  receipts: readonly CommandReceipt[];
  nextAfter?: CursorString;
}>;

export type AgentTurnSubmission = Readonly<{
  run: AgentRun;
  message?: AgentConversationMessage;
  replayed: boolean;
}>;

export type AgentRunPage = Readonly<{
  projectId: DurableID;
  runs: readonly AgentRun[];
}>;

export type AgentTurnPage = Readonly<{
  projectId: DurableID;
  runId: DurableID;
  turns: readonly AgentTurn[];
  nextBefore?: CursorString;
}>;

export interface AgentBridgePort {
  availability(signal?: AbortSignal): Promise<AgentAvailability>;
  list(projectId: DurableID, limit?: number, signal?: AbortSignal): Promise<AgentRunPage>;
  begin(projectId: DurableID, input: BeginAgentRunInput, signal?: AbortSignal): Promise<AgentTurnSubmission>;
  show(projectId: DurableID, runId: DurableID, signal?: AbortSignal): Promise<AgentRun>;
  continue(
    projectId: DurableID,
    runId: DurableID,
    input: ContinueAgentRunInput,
    signal?: AbortSignal,
  ): Promise<AgentTurnSubmission>;
  conversation(
    projectId: DurableID,
    runId: DurableID,
    input?: Readonly<{ after?: CursorString; limit?: number }>,
    signal?: AbortSignal,
  ): Promise<AgentConversationPage>;
  turns(
    projectId: DurableID,
    runId: DurableID,
    input?: Readonly<{ before?: CursorString; limit?: number }>,
    signal?: AbortSignal,
  ): Promise<AgentTurnPage>;
  receipts(
    projectId: DurableID,
    runId: DurableID,
    turnId: DurableID,
    input?: Readonly<{ after?: CursorString; limit?: number }>,
    signal?: AbortSignal,
  ): Promise<TurnReceiptPage>;
  interrupt(projectId: DurableID, run: AgentRun, requestId: string, signal?: AbortSignal): Promise<AgentRun>;
  cancel(projectId: DurableID, run: AgentRun, requestId: string, signal?: AbortSignal): Promise<AgentRun>;
  watchPresentation(
    projectId: DurableID,
    runId: DurableID,
    turnId: DurableID,
    receive: (event: AgentPresentation) => void,
    signal: AbortSignal,
  ): Promise<void>;
}

export function createAgentBridgePort(): AgentBridgePort {
  return {
    availability: async (signal) => {
      const response = await showLocalAgentAvailability({ signal });
      if (response.status !== 200)
        throw await responseError("show local Agent availability", response.status, response.data);
      return normalizeAgentAvailability(response.data);
    },
    list: async (projectId, limit = 10, signal) => {
      if (!isBoundedInteger(limit, 1, 20)) throw new Error("Agent run page limit is invalid");
      const response = await listCreatorAgentRuns(projectId, { limit }, { signal });
      if (response.status !== 200) throw await responseError("list Agent runs", response.status, response.data);
      const record = asRecord(response.data);
      if (durableID(record.projectId) !== projectId || !Array.isArray(record.runs) || record.runs.length > 20) {
        throw new Error("Agent run page is invalid");
      }
      const runs = record.runs.map(normalizeAgentRun);
      for (const run of runs) {
        if (run.projectId !== projectId) throw new Error("Agent run page identity is invalid");
      }
      return { projectId, runs };
    },
    begin: async (projectId, input, signal) => {
      const response = await beginCreatorAgentRun(projectId, normalizeBeginInput(input), { signal });
      if (response.status !== 200) throw await responseError("begin Agent run", response.status, response.data);
      return normalizeSubmission(response.data);
    },
    show: async (projectId, runId, signal) => {
      const response = await showCreatorAgentRun(projectId, runId, { signal });
      if (response.status !== 200) throw await responseError("show Agent run", response.status, response.data);
      const run = normalizeAgentRun(response.data);
      if (run.projectId !== projectId || run.id !== runId) throw new Error("Agent run identity is invalid");
      return run;
    },
    continue: async (projectId, runId, input, signal) => {
      const response = await continueCreatorAgentRun(projectId, runId, normalizeContinueInput(input), { signal });
      if (response.status !== 200) throw await responseError("continue Agent run", response.status, response.data);
      const result = normalizeSubmission(response.data);
      if (result.run.projectId !== projectId || result.run.id !== runId)
        throw new Error("Agent run identity is invalid");
      return result;
    },
    conversation: async (projectId, runId, input = {}, signal) => {
      if (input.limit !== undefined && !isBoundedInteger(input.limit, 1, 100)) {
        throw new Error("Agent conversation limit is invalid");
      }
      const response = await listCreatorAgentConversation(projectId, runId, input, { signal });
      if (response.status !== 200) {
        throw await responseError("list Agent conversation", response.status, response.data);
      }
      return normalizeConversationPage(response.data, projectId, runId);
    },
    turns: async (projectId, runId, input = {}, signal) => {
      if (input.limit !== undefined && !isBoundedInteger(input.limit, 1, 100)) {
        throw new Error("Agent turn page limit is invalid");
      }
      const response = await listCreatorAgentTurns(projectId, runId, input, { signal });
      if (response.status !== 200) throw await responseError("list Agent turns", response.status, response.data);
      return normalizeAgentTurnPage(response.data, projectId, runId);
    },
    receipts: async (projectId, runId, turnId, input = {}, signal) => {
      if (input.limit !== undefined && !isBoundedInteger(input.limit, 1, 100)) {
        throw new Error("Agent receipt limit is invalid");
      }
      const response = await listCreatorAgentTurnReceipts(projectId, runId, turnId, input, { signal });
      if (response.status !== 200) throw await responseError("list Agent receipts", response.status, response.data);
      return normalizeTurnReceiptPage(response.data, projectId, runId, turnId);
    },
    interrupt: async (projectId, run, requestId, signal) => {
      validateRequestID(requestId);
      const response = await interruptCreatorAgentTurn(
        projectId,
        run.id,
        run.currentTurn.id,
        { requestId, expectedGeneration: run.currentTurn.generation },
        { signal },
      );
      if (response.status !== 200) throw await responseError("interrupt Agent turn", response.status, response.data);
      return normalizeTransition(response.data, projectId, run.id);
    },
    cancel: async (projectId, run, requestId, signal) => {
      validateRequestID(requestId);
      const response = await cancelCreatorAgentRun(
        projectId,
        run.id,
        run.currentTurn.id,
        { requestId, expectedGeneration: run.currentTurn.generation },
        { signal },
      );
      if (response.status !== 200) throw await responseError("cancel Agent run", response.status, response.data);
      return normalizeTransition(response.data, projectId, run.id);
    },
    watchPresentation: async (projectId, runId, turnId, receive, signal) => {
      const response = await fetch(getWatchCreatorAgentPresentationUrl(projectId, runId, turnId), {
        headers: { accept: "text/event-stream" },
        signal,
      });
      for await (const message of readServerEvents(response)) {
        if (message.event !== "presentation") continue;
        const event = normalizePresentation(message.data);
        if (event.runId !== runId || event.turnId !== turnId) throw new Error("Agent presentation identity is invalid");
        receive(event);
      }
    },
  };
}

function normalizeAgentAvailability(value: unknown): AgentAvailability {
  const record = asRecord(value);
  if (
    record.adapterId !== "codex-cli-v1" ||
    record.promptVersion !== "open-cut-agent-v1" ||
    (record.state !== "available" &&
      record.state !== "missing" &&
      record.state !== "unauthenticated" &&
      record.state !== "incompatible")
  ) {
    throw new Error("local Agent availability is invalid");
  }
  const version = optionalBoundedText(record.version, 128, "local Agent version");
  if ((record.state === "available") !== (version !== undefined)) {
    throw new Error("local Agent availability version is invalid");
  }
  return {
    adapterId: record.adapterId,
    promptVersion: record.promptVersion,
    state: record.state,
    ...(version === undefined ? {} : { version }),
  };
}

function normalizeBeginInput(input: BeginAgentRunInput): BeginAgentRunInput {
  validateRequestID(input.requestId);
  validateMessage(input.message);
  if (input.sequenceId !== undefined) durableID(input.sequenceId);
  return { ...input, attachments: normalizeAgentContextAttachments(input.attachments) };
}

function normalizeContinueInput(input: ContinueAgentRunInput): ContinueAgentRunInput {
  validateRequestID(input.requestId);
  revisionString(input.expectedGeneration);
  validateMessage(input.message);
  if (input.sequenceId !== undefined) durableID(input.sequenceId);
  return { ...input, attachments: normalizeAgentContextAttachments(input.attachments) };
}

function normalizeSubmission(value: unknown): AgentTurnSubmission {
  const record = asRecord(value);
  if (typeof record.replayed !== "boolean") throw new Error("Agent submission replay state is invalid");
  const run = normalizeAgentRun(record.run);
  const message = record.message === undefined ? undefined : normalizeConversationMessage(record.message);
  if (
    message &&
    (message.projectId !== run.projectId || message.runId !== run.id || message.turnId !== run.currentTurn.id)
  ) {
    throw new Error("Agent submission message identity is invalid");
  }
  return { run, ...(message ? { message } : {}), replayed: record.replayed };
}

function normalizeTransition(value: unknown, projectId: DurableID, runId: DurableID): AgentRun {
  const result = normalizeSubmission(value).run;
  if (result.projectId !== projectId || result.id !== runId) throw new Error("Agent run identity is invalid");
  return result;
}

function normalizeAgentRun(value: unknown): AgentRun {
  const record = asRecord(value);
  const status = agentRunStatus(record.status);
  const waitingReason = optionalBoundedText(record.waitingReason, 128, "Agent waiting reason");
  const completedAt = record.completedAt === undefined ? undefined : timestamp(record.completedAt);
  return {
    id: durableID(record.id),
    projectId: durableID(record.projectId),
    intent: boundedUTF8(record.intent, 1, 32_768, "Agent intent"),
    ...(record.agentId === undefined ? {} : { agentId: durableID(record.agentId) }),
    status,
    ...(waitingReason === undefined ? {} : { waitingReason }),
    currentTurn: normalizeAgentTurn(record.currentTurn),
    activityCursor: cursorString(record.activityCursor),
    createdAt: timestamp(record.createdAt),
    updatedAt: timestamp(record.updatedAt),
    ...(completedAt === undefined ? {} : { completedAt }),
  };
}

function normalizeAgentTurn(value: unknown): AgentTurn {
  const record = asRecord(value);
  return {
    id: durableID(record.id),
    generation: revisionString(record.generation),
    ...(record.sequenceId === undefined ? {} : { sequenceId: durableID(record.sequenceId) }),
    status: agentTurnStatus(record.status),
    startedAt: timestamp(record.startedAt),
    ...(record.endedAt === undefined ? {} : { endedAt: timestamp(record.endedAt) }),
  };
}

function normalizeConversationPage(value: unknown, projectId: DurableID, runId: DurableID): AgentConversationPage {
  const record = asRecord(value);
  if (!Array.isArray(record.messages) || record.messages.length > 100)
    throw new Error("Agent conversation page is invalid");
  const messages = record.messages.map(normalizeConversationMessage);
  let previous = 0n;
  for (const message of messages) {
    if (message.projectId !== projectId || message.runId !== runId || BigInt(message.ordinal) <= previous) {
      throw new Error("Agent conversation ordering is invalid");
    }
    previous = BigInt(message.ordinal);
  }
  const nextAfter = record.nextAfter === undefined ? undefined : cursorString(record.nextAfter);
  if (durableID(record.projectId) !== projectId || durableID(record.runId) !== runId) {
    throw new Error("Agent conversation identity is invalid");
  }
  return { projectId, runId, messages, ...(nextAfter === undefined ? {} : { nextAfter }) };
}

function normalizeAgentTurnPage(value: unknown, projectId: DurableID, runId: DurableID): AgentTurnPage {
  const record = asRecord(value);
  if (!Array.isArray(record.turns) || record.turns.length > 100) throw new Error("Agent turn page is invalid");
  if (durableID(record.projectId) !== projectId || durableID(record.runId) !== runId) {
    throw new Error("Agent turn page identity is invalid");
  }
  const turns = record.turns.map(normalizeAgentTurn);
  let previous: bigint | undefined;
  for (const turn of turns) {
    const generation = BigInt(turn.generation);
    if (previous !== undefined && generation >= previous) throw new Error("Agent turn page ordering is invalid");
    previous = generation;
  }
  const nextBefore = record.nextBefore === undefined ? undefined : cursorString(record.nextBefore);
  return { projectId, runId, turns, ...(nextBefore === undefined ? {} : { nextBefore }) };
}

function normalizeConversationMessage(value: unknown): AgentConversationMessage {
  const record = asRecord(value);
  if (record.role !== "creator" && record.role !== "agent" && record.role !== "notice")
    throw new Error("Agent conversation role is invalid");
  const text = boundedUTF8(record.text, 1, 262_144, "Agent conversation text");
  if (record.role === "notice" && text !== "context-rebuilt") throw new Error("Agent conversation notice is invalid");
  return {
    id: durableID(record.id),
    projectId: durableID(record.projectId),
    runId: durableID(record.runId),
    turnId: durableID(record.turnId),
    ordinal: cursorString(record.ordinal),
    role: record.role,
    text,
    attachments: normalizeAgentContextAttachments(record.attachments),
    createdAt: timestamp(record.createdAt),
  };
}

function normalizeTurnReceiptPage(
  value: unknown,
  projectId: DurableID,
  runId: DurableID,
  turnId: DurableID,
): TurnReceiptPage {
  const record = asRecord(value);
  if (!Array.isArray(record.receipts) || record.receipts.length > 100) throw new Error("Agent receipt page is invalid");
  if (
    durableID(record.projectId) !== projectId ||
    durableID(record.runId) !== runId ||
    durableID(record.turnId) !== turnId
  ) {
    throw new Error("Agent receipt page identity is invalid");
  }
  const receipts = record.receipts.map(normalizeCommandReceipt);
  let previous = 0n;
  for (const receipt of receipts) {
    if (
      receipt.projectId !== projectId ||
      receipt.runId !== runId ||
      receipt.turnId !== turnId ||
      BigInt(receipt.ordinal) <= previous
    ) {
      throw new Error("Agent receipt ordering is invalid");
    }
    previous = BigInt(receipt.ordinal);
  }
  const nextAfter = record.nextAfter === undefined ? undefined : cursorString(record.nextAfter);
  return { projectId, runId, turnId, receipts, ...(nextAfter === undefined ? {} : { nextAfter }) };
}

function normalizeCommandReceipt(value: unknown): CommandReceipt {
  const record = asRecord(value);
  if (
    record.schema !== "open-cut/command-receipt/v2" ||
    (record.class !== "evidence" && record.class !== "outcome") ||
    !isCommandReceiptStatus(record.status) ||
    !Array.isArray(record.resultRefs) ||
    record.resultRefs.length > 256
  ) {
    throw new Error("Agent command receipt is invalid");
  }
  const ordinal = cursorString(record.ordinal);
  if (BigInt(ordinal) < 1n) throw new Error("Agent receipt ordinal is invalid");
  const requestId =
    record.requestId === undefined ? undefined : boundedUTF8(record.requestId, 1, 128, "request identity");
  if (requestId !== undefined) validateRequestID(requestId);
  return {
    schema: record.schema,
    id: durableID(record.id),
    projectId: durableID(record.projectId),
    runId: durableID(record.runId),
    turnId: durableID(record.turnId),
    ordinal,
    class: record.class,
    command: boundedUTF8(record.command, 3, 128, "receipt command"),
    commandFingerprint: digestString(record.commandFingerprint),
    inputDigest: digestString(record.inputDigest),
    ...(requestId === undefined ? {} : { requestId }),
    resultDigest: digestString(record.resultDigest),
    status: record.status,
    resultRefs: record.resultRefs.map(normalizeCommandReceiptRef),
    ...(record.projectRevision === undefined ? {} : { projectRevision: positiveRevision(record.projectRevision) }),
    ...(record.activityCursor === undefined ? {} : { activityCursor: positiveCursor(record.activityCursor) }),
    createdAt: timestamp(record.createdAt),
  };
}

function isCommandReceiptStatus(value: unknown): value is CommandReceipt["status"] {
  return (
    value === "succeeded" ||
    value === "accepted" ||
    value === "waiting" ||
    value === "approval-required" ||
    value === "conflict" ||
    value === "not-found" ||
    value === "unavailable" ||
    value === "incompatible" ||
    value === "invalid" ||
    value === "failed"
  );
}

function normalizeCommandReceiptRef(value: unknown): CommandReceiptRef {
  const record = asRecord(value);
  return {
    kind: boundedUTF8(record.kind, 1, 64, "receipt reference kind"),
    id: boundedUTF8(record.id, 1, 128, "receipt reference identity"),
    ...(record.revision === undefined ? {} : { revision: positiveRevision(record.revision) }),
  };
}

function normalizeAgentContextAttachments(value: unknown): readonly AgentContextAttachment[] {
  if (!Array.isArray(value) || value.length > 64) throw new Error("Agent context attachments are invalid");
  const attachments = value.map(normalizeAgentContextAttachment);
  const encoded = attachments.map((attachment) => JSON.stringify(attachment));
  if (new Set(encoded).size !== encoded.length || encoder.encode(encoded.join("")).byteLength > 16 * 1024) {
    throw new Error("Agent context attachments are invalid");
  }
  return attachments;
}

function normalizeAgentContextAttachment(value: unknown): AgentContextAttachment {
  const record = asRecord(value);
  if (
    record.kind === "asset" ||
    record.kind === "narrative-node" ||
    record.kind === "clip" ||
    record.kind === "caption" ||
    record.kind === "track"
  ) {
    const entity = asRecord(record.entity);
    return { kind: record.kind, entity: { id: durableID(entity.id), revision: positiveRevision(entity.revision) } };
  }
  if (record.kind === "transcript-segment") {
    const transcript = asRecord(record.transcript);
    return {
      kind: record.kind,
      transcript: { artifactId: durableID(transcript.artifactId), segmentId: durableID(transcript.segmentId) },
    };
  }
  if (record.kind === "sequence-point") {
    const point = asRecord(record.point);
    return {
      kind: record.kind,
      point: {
        sequenceId: durableID(point.sequenceId),
        revision: positiveRevision(point.revision),
        time: normalizeRational(point.time),
      },
    };
  }
  if (record.kind === "sequence-range") {
    const range = asRecord(record.range);
    return {
      kind: record.kind,
      range: {
        sequenceId: durableID(range.sequenceId),
        revision: positiveRevision(range.revision),
        range: normalizeTimeRange(range.range),
      },
    };
  }
  throw new Error("Agent context attachment kind is invalid");
}

function positiveRevision(value: unknown): RevisionString {
  const revision = revisionString(value);
  if (BigInt(revision) < 1n) throw new Error("revision must be positive");
  return revision;
}

function positiveCursor(value: unknown): CursorString {
  const cursor = cursorString(value);
  if (BigInt(cursor) < 1n) throw new Error("cursor must be positive");
  return cursor;
}

function normalizePresentation(value: unknown): AgentPresentation {
  const record = asRecord(value);
  const kind = agentPresentationKind(record.kind);
  const tool = record.tool === undefined ? undefined : agentPresentationTool(record.tool);
  if ((kind === "tool-started" || kind === "tool-completed") !== (tool !== undefined)) {
    throw new Error("Agent presentation tool is invalid");
  }
  return {
    runId: durableID(record.runId),
    turnId: durableID(record.turnId),
    sequence: cursorString(record.sequence),
    kind,
    ...(tool === undefined ? {} : { tool }),
  };
}

function agentRunStatus(value: unknown): AgentRunStatus {
  if (
    value === "authorizing" ||
    value === "active" ||
    value === "waiting" ||
    value === "paused" ||
    value === "completed" ||
    value === "failed" ||
    value === "cancelled"
  )
    return value;
  throw new Error("Agent run status is invalid");
}

function agentTurnStatus(value: unknown): AgentTurnStatus {
  if (
    value === "starting" ||
    value === "active" ||
    value === "detached" ||
    value === "completed" ||
    value === "failed" ||
    value === "cancelled" ||
    value === "superseded"
  )
    return value;
  throw new Error("Agent turn status is invalid");
}

function agentPresentationKind(value: unknown): AgentPresentation["kind"] {
  if (
    value === "turn-started" ||
    value === "context-rebuilt" ||
    value === "tool-started" ||
    value === "tool-completed" ||
    value === "message-completed" ||
    value === "turn-completed" ||
    value === "turn-failed"
  )
    return value;
  throw new Error("Agent presentation kind is invalid");
}

function agentPresentationTool(value: unknown): NonNullable<AgentPresentation["tool"]> {
  if (
    value === "command" ||
    value === "file-change" ||
    value === "reasoning" ||
    value === "web-search" ||
    value === "plan"
  )
    return value;
  throw new Error("Agent presentation tool is invalid");
}

function validateRequestID(value: string): void {
  if (!requestIDPattern.test(value)) throw new Error("Agent request identity is invalid");
}

function validateMessage(value: string): void {
  boundedUTF8(value, 1, 32_768, "Creator message");
}

function boundedUTF8(value: unknown, minimum: number, maximum: number, name: string): string {
  if (typeof value !== "string") throw new Error(`${name} is invalid`);
  const length = encoder.encode(value).byteLength;
  if (length < minimum || length > maximum) throw new Error(`${name} is invalid`);
  return value;
}

function optionalBoundedText(value: unknown, maximum: number, name: string): string | undefined {
  if (value === undefined) return undefined;
  return boundedUTF8(value, 0, maximum, name);
}
