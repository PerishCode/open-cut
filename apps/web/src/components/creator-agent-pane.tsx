import {
  Button,
  ControlStrip,
  MessageContent,
  PanelDock,
  ResourceCard,
  Stack,
  Status,
  Text,
  TextAreaField,
} from "@open-cut/components";
import {
  type AgentAvailability,
  type AgentContextAttachment,
  type AgentConversationMessage,
  type AgentPresentation,
  type AgentRun,
  type AgentTurn,
  type CommandReceipt,
  type CommandReceiptRef,
  type CursorString,
  type DurableID,
  useContracts,
} from "@open-cut/contracts";
import { useCallback, useEffect, useRef, useState } from "react";

import type { CreatorAgentContextCandidate } from "./creator-agent-context.js";

type AgentPaneState = Readonly<{
  availability?: AgentAvailability;
  runs: readonly AgentRun[];
  selected?: AgentRun;
  messages: readonly AgentConversationMessage[];
  nextAfter?: CursorString;
  turns: readonly AgentTurn[];
  turnNextBefore?: CursorString;
  selectedTurn?: AgentTurn;
  receipts: readonly CommandReceipt[];
  receiptNextAfter?: CursorString;
  presentation?: AgentPresentation;
  focusNotice?: string;
  loading: boolean;
  submitting: boolean;
  error?: Error;
}>;

type ContextKeys = readonly string[];

const initialState: AgentPaneState = {
  runs: [],
  messages: [],
  turns: [],
  receipts: [],
  loading: true,
  submitting: false,
};

export function CreatorAgentPane({
  contextCandidates,
  onAddPlayheadContext,
  onAddTimelineContext,
  onFocusReceiptRef,
  projectId,
  sequenceId,
}: {
  contextCandidates: readonly CreatorAgentContextCandidate[];
  onAddPlayheadContext?: () => void;
  onAddTimelineContext?: () => void;
  onFocusReceiptRef?: (ref: CommandReceiptRef) => string;
  projectId: DurableID;
  sequenceId: DurableID;
}) {
  const contracts = useContracts();
  const [state, setState] = useState<AgentPaneState>(initialState);
  const [message, setMessage] = useState("");
  const [contextKeys, setContextKeys] = useState<ContextKeys>([]);
  const [recentRunsExpanded, setRecentRunsExpanded] = useState(false);
  const [receiptsExpanded, setReceiptsExpanded] = useState(false);
  const latestMessageRef = useRef<HTMLElement>(null);
  const latestOutcomeRef = useRef<HTMLElement>(null);
  const lastRevealedItemRef = useRef<DurableID | undefined>(undefined);

  const loadRun = useCallback(
    async (runId: DurableID, signal?: AbortSignal) => {
      const run = await contracts.agent.show(projectId, runId, signal);
      const [conversation, turnPage] = await Promise.all([
        contracts.agent.conversation(projectId, runId, { limit: 100 }, signal),
        contracts.agent.turns(projectId, runId, { limit: 100 }, signal),
      ]);
      const selectedTurn = turnPage.turns.find((turn) => turn.id === run.currentTurn.id) ?? run.currentTurn;
      const receipts = await contracts.agent.receipts(projectId, runId, selectedTurn.id, { limit: 100 }, signal);
      if (signal?.aborted) return;
      setState((current) => ({
        ...current,
        runs: current.runs.map((candidate) => (candidate.id === run.id ? run : candidate)),
        selected: run,
        messages: conversation.messages,
        nextAfter: conversation.nextAfter,
        turns: turnPage.turns,
        turnNextBefore: turnPage.nextBefore,
        selectedTurn,
        receipts: receipts.receipts,
        receiptNextAfter: receipts.nextAfter,
        presentation: undefined,
        loading: false,
        error: undefined,
      }));
    },
    [contracts, projectId],
  );

  const load = useCallback(
    async (signal?: AbortSignal) => {
      setState((current) => ({ ...current, loading: true, error: undefined }));
      try {
        const [availability, page] = await Promise.all([
          contracts.agent.availability(signal),
          contracts.agent.list(projectId, 10, signal),
        ]);
        if (signal?.aborted) return;
        const selectedId = page.runs[0]?.id;
        setState((current) => ({
          ...current,
          availability,
          runs: page.runs,
          selected: undefined,
          messages: [],
          nextAfter: undefined,
          turns: [],
          turnNextBefore: undefined,
          selectedTurn: undefined,
          receipts: [],
          receiptNextAfter: undefined,
          loading: selectedId !== undefined,
        }));
        if (selectedId) await loadRun(selectedId, signal);
        else
          setState((current) => ({
            ...current,
            messages: [],
            nextAfter: undefined,
            turns: [],
            turnNextBefore: undefined,
            selectedTurn: undefined,
            receipts: [],
            receiptNextAfter: undefined,
            loading: false,
          }));
      } catch (value) {
        if (!signal?.aborted) {
          setState((current) => ({ ...current, loading: false, error: asError(value) }));
        }
      }
    },
    [contracts, loadRun, projectId],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  const selectRun = useCallback(
    async (runId: DurableID) => {
      setState((current) => ({ ...current, loading: true, error: undefined }));
      setRecentRunsExpanded(false);
      setReceiptsExpanded(false);
      try {
        await loadRun(runId);
      } catch (value) {
        setState((current) => ({ ...current, loading: false, error: asError(value) }));
      }
    },
    [loadRun],
  );

  const reconcileSelected = useCallback(async () => {
    const selected = state.selected;
    if (!selected) return;
    try {
      await loadRun(selected.id);
    } catch (value) {
      setState((current) => ({ ...current, error: asError(value) }));
    }
  }, [loadRun, state.selected]);

  useEffect(() => {
    const run = state.selected;
    if (!run || (run.currentTurn.status !== "starting" && run.currentTurn.status !== "active")) return;
    const controller = new AbortController();
    void contracts.agent
      .watchPresentation(
        projectId,
        run.id,
        run.currentTurn.id,
        (presentation) => setState((current) => ({ ...current, presentation })),
        controller.signal,
      )
      .catch((value) => {
        if (!controller.signal.aborted) setState((current) => ({ ...current, error: asError(value) }));
      })
      .finally(() => {
        if (!controller.signal.aborted) void reconcileSelected();
      });
    return () => controller.abort();
  }, [contracts, projectId, reconcileSelected, state.selected]);

  const submit = useCallback(async () => {
    const text = message.trim();
    if (!text || state.submitting || state.availability?.state !== "available") return;
    setState((current) => ({ ...current, submitting: true, error: undefined }));
    try {
      const attachments = selectedAttachments(contextCandidates, contextKeys);
      const submission = state.selected
        ? await contracts.agent.continue(projectId, state.selected.id, {
            requestId: requestID("continue"),
            expectedGeneration: state.selected.currentTurn.generation,
            message: text,
            sequenceId,
            attachments,
          })
        : await contracts.agent.begin(projectId, {
            requestId: requestID("begin"),
            message: text,
            sequenceId,
            attachments,
          });
      setMessage("");
      setContextKeys([]);
      setReceiptsExpanded(false);
      setState((current) => ({
        ...current,
        runs: [submission.run, ...current.runs.filter((run) => run.id !== submission.run.id)],
        selected: submission.run,
        messages: submission.message ? [...current.messages, submission.message] : current.messages,
        nextAfter: undefined,
        turns: [
          submission.run.currentTurn,
          ...current.turns.filter((turn) => turn.id !== submission.run.currentTurn.id),
        ],
        turnNextBefore: current.turnNextBefore,
        selectedTurn: submission.run.currentTurn,
        receipts: [],
        receiptNextAfter: undefined,
        presentation: undefined,
        submitting: false,
      }));
    } catch (value) {
      setState((current) => ({ ...current, submitting: false, error: asError(value) }));
    }
  }, [
    contextCandidates,
    contextKeys,
    contracts,
    message,
    projectId,
    sequenceId,
    state.availability,
    state.selected,
    state.submitting,
  ]);

  const transition = useCallback(
    async (kind: "interrupt" | "cancel") => {
      if (!state.selected || state.submitting) return;
      setState((current) => ({ ...current, submitting: true, error: undefined }));
      try {
        const run =
          kind === "interrupt"
            ? await contracts.agent.interrupt(projectId, state.selected, requestID("interrupt"))
            : await contracts.agent.cancel(projectId, state.selected, requestID("cancel"));
        setState((current) => ({
          ...current,
          runs: current.runs.map((candidate) => (candidate.id === run.id ? run : candidate)),
          selected: run,
          turns: current.turns.map((turn) => (turn.id === run.currentTurn.id ? run.currentTurn : turn)),
          selectedTurn: current.selectedTurn?.id === run.currentTurn.id ? run.currentTurn : current.selectedTurn,
          presentation: undefined,
          submitting: false,
        }));
      } catch (value) {
        setState((current) => ({ ...current, submitting: false, error: asError(value) }));
      }
    },
    [contracts, projectId, state.selected, state.submitting],
  );

  const loadMore = useCallback(async () => {
    if (!state.selected || !state.nextAfter || state.loading) return;
    setState((current) => ({ ...current, loading: true }));
    try {
      const page = await contracts.agent.conversation(projectId, state.selected.id, {
        after: state.nextAfter,
        limit: 100,
      });
      setState((current) => ({
        ...current,
        messages: [...current.messages, ...page.messages],
        nextAfter: page.nextAfter,
        loading: false,
      }));
    } catch (value) {
      setState((current) => ({ ...current, loading: false, error: asError(value) }));
    }
  }, [contracts, projectId, state.loading, state.nextAfter, state.selected]);

  const loadMoreReceipts = useCallback(async () => {
    if (!state.selected || !state.selectedTurn || !state.receiptNextAfter || state.loading) return;
    setState((current) => ({ ...current, loading: true }));
    try {
      const page = await contracts.agent.receipts(projectId, state.selected.id, state.selectedTurn.id, {
        after: state.receiptNextAfter,
        limit: 100,
      });
      setState((current) => ({
        ...current,
        receipts: [...current.receipts, ...page.receipts],
        receiptNextAfter: page.nextAfter,
        loading: false,
      }));
    } catch (value) {
      setState((current) => ({ ...current, loading: false, error: asError(value) }));
    }
  }, [contracts, projectId, state.loading, state.receiptNextAfter, state.selected, state.selectedTurn]);

  const selectTurn = useCallback(
    async (turn: AgentTurn) => {
      if (!state.selected || state.loading || turn.id === state.selectedTurn?.id) return;
      setState((current) => ({ ...current, loading: true, error: undefined }));
      setReceiptsExpanded(false);
      try {
        const page = await contracts.agent.receipts(projectId, state.selected.id, turn.id, { limit: 100 });
        setState((current) => ({
          ...current,
          selectedTurn: turn,
          receipts: page.receipts,
          receiptNextAfter: page.nextAfter,
          loading: false,
        }));
      } catch (value) {
        setState((current) => ({ ...current, loading: false, error: asError(value) }));
      }
    },
    [contracts, projectId, state.loading, state.selected, state.selectedTurn],
  );

  const loadOlderTurns = useCallback(async () => {
    if (!state.selected || !state.turnNextBefore || state.loading) return;
    setState((current) => ({ ...current, loading: true, error: undefined }));
    try {
      const page = await contracts.agent.turns(projectId, state.selected.id, {
        before: state.turnNextBefore,
        limit: 100,
      });
      setState((current) => ({
        ...current,
        turns: [...current.turns, ...page.turns],
        turnNextBefore: page.nextBefore,
        loading: false,
      }));
    } catch (value) {
      setState((current) => ({ ...current, loading: false, error: asError(value) }));
    }
  }, [contracts, projectId, state.loading, state.selected, state.turnNextBefore]);

  const active = state.selected?.currentTurn.status === "starting" || state.selected?.currentTurn.status === "active";
  const canContinue = state.selected?.status === "paused" || state.selected?.status === "waiting";
  const terminal = state.selected
    ? state.selected.status === "completed" ||
      state.selected.status === "failed" ||
      state.selected.status === "cancelled"
    : false;
  const canSubmit = state.availability?.state === "available" && !state.submitting && (!state.selected || canContinue);
  const latestOutcome = [...state.receipts].reverse().find((receipt) => receipt.class === "outcome");
  const recentRuns = state.runs.filter((run) => run.id !== state.selected?.id);
  const latestMessage = state.messages.at(-1);
  const latestRevealMessageId = latestMessage?.role === "creator" ? undefined : latestMessage?.id;
  const latestRevealItem = latestOutcome?.id ?? latestRevealMessageId;

  useEffect(() => {
    if (!latestRevealItem || lastRevealedItemRef.current === latestRevealItem) return;
    const target = latestOutcome ? latestOutcomeRef.current : latestMessageRef.current;
    if (!target) return;
    lastRevealedItemRef.current = latestRevealItem;
    target.scrollIntoView({ block: latestOutcome ? "nearest" : "start", inline: "nearest" });
  }, [latestOutcome, latestRevealItem]);

  return (
    <PanelDock
      footer={
        <Stack spacing="compact">
          {onAddTimelineContext || onAddPlayheadContext ? (
            <ControlStrip label="Quick Agent context" summary="QUICK CONTEXT">
              {onAddTimelineContext ? (
                <Button disabled={!canSubmit} onPress={onAddTimelineContext}>
                  Add visible timeline
                </Button>
              ) : null}
              {onAddPlayheadContext ? (
                <Button disabled={!canSubmit} onPress={onAddPlayheadContext}>
                  Add playhead
                </Button>
              ) : null}
            </ControlStrip>
          ) : null}
          {contextCandidates.length > 0 ? (
            <ControlStrip
              hint={contextKeys.length > 0 ? `${contextKeys.length} attached` : "Optional"}
              label="Agent context attachments"
              summary="@ CONTEXT"
            >
              {contextCandidates.map((candidate) => {
                const selected = contextKeys.includes(candidate.key);
                return (
                  <Button
                    disabled={!canSubmit}
                    key={candidate.key}
                    onPress={() =>
                      setContextKeys((current) =>
                        selected ? current.filter((key) => key !== candidate.key) : [...current, candidate.key],
                      )
                    }
                  >
                    {selected ? `@ ${candidate.label} · remove` : `Add @ ${candidate.label}`}
                  </Button>
                );
              })}
            </ControlStrip>
          ) : null}
          <TextAreaField
            disabled={!canSubmit}
            keyboardShortcuts={canSubmit ? "Control+Enter Meta+Enter" : undefined}
            label={`${state.selected ? "Continue this task" : "New task"}${canSubmit ? " · Ctrl/⌘ Enter" : ""}`}
            maxLength={8000}
            placeholder={
              canContinue || !state.selected
                ? "Tell the Agent what to write or change…"
                : "Wait for this Turn to finish."
            }
            rows={5}
            value={message}
            onChange={setMessage}
            onKeyDown={(event) => {
              if (
                event.key !== "Enter" ||
                (!event.ctrlKey && !event.metaKey) ||
                event.altKey ||
                event.shiftKey ||
                event.repeat ||
                event.nativeEvent.isComposing
              ) {
                return;
              }
              event.preventDefault();
              void submit();
            }}
          />
          <Button disabled={!canSubmit || message.trim() === ""} variant="primary" onPress={() => void submit()}>
            {state.submitting ? "Submitting…" : state.selected ? "Continue" : "Start task"}
          </Button>
        </Stack>
      }
      header={
        <ControlStrip hint={availabilityText(state.availability)} label="Agent controls" summary="LOCAL AGENT">
          <Button disabled={state.loading || state.submitting} variant="quiet" onPress={() => void load()}>
            Refresh
          </Button>
          <Button
            disabled={state.submitting}
            onPress={() => {
              setState((current) => ({
                ...current,
                selected: undefined,
                messages: [],
                nextAfter: undefined,
                turns: [],
                turnNextBefore: undefined,
                selectedTurn: undefined,
                receipts: [],
                receiptNextAfter: undefined,
                error: undefined,
              }));
              setMessage("");
              setContextKeys([]);
              setRecentRunsExpanded(false);
              setReceiptsExpanded(false);
            }}
          >
            New task
          </Button>
        </ControlStrip>
      }
      label="Agent collaboration"
    >
      <Stack spacing="compact">
        {state.selected ? (
          <ControlStrip
            label="Selected Agent task"
            summary={`TASK · TURN ${state.selected.currentTurn.generation} · ${state.selected.intent}`}
          >
            {state.presentation ? <Status state="pending">{presentationText(state.presentation)}</Status> : null}
            <Status state={runStatusState(state.selected)}>{runStatusLabel(state.selected)}</Status>
            {active ? (
              <>
                <Button disabled={state.submitting} onPress={() => void transition("interrupt")}>
                  Stop
                </Button>
                <Button disabled={state.submitting} onPress={() => void transition("cancel")}>
                  Cancel task
                </Button>
              </>
            ) : !terminal ? (
              <Button disabled={state.submitting} onPress={() => void transition("cancel")}>
                Cancel task
              </Button>
            ) : null}
          </ControlStrip>
        ) : (
          <Text>Describe a new writing or editing task.</Text>
        )}
        {latestOutcome ? (
          <ResourceCard
            details={outcomeDetails(latestOutcome)}
            elementRef={latestOutcomeRef}
            emphasis="strong"
            eyebrow={`LATEST OUTCOME · #${latestOutcome.ordinal}`}
            status={<Status state={receiptStatusState(latestOutcome)}>{receiptStatusLabel(latestOutcome)}</Status>}
            title={outcomeTitle(latestOutcome)}
          />
        ) : null}
        {state.messages.length > 0 ? <Text tone="eyebrow">CONVERSATION · {state.messages.length} MESSAGES</Text> : null}
        {state.messages.map((entry) => (
          <ResourceCard
            details={entry.attachments.map((attachment) => `@ ${attachmentLabel(attachment)}`)}
            elementRef={!latestOutcome && entry.id === latestRevealMessageId ? latestMessageRef : undefined}
            emphasis={entry.role === "agent" ? "default" : "quiet"}
            eyebrow={`${messageRole(entry)} · MESSAGE #${entry.ordinal}`}
            key={entry.id}
            title={messageTitle(entry)}
          >
            {entry.role === "agent" ? (
              <MessageContent text={entry.text} />
            ) : (
              <Text>
                {entry.role === "notice" ? "Agent context was safely rebuilt from this conversation." : entry.text}
              </Text>
            )}
          </ResourceCard>
        ))}
        {state.nextAfter ? (
          <Button disabled={state.loading} onPress={() => void loadMore()}>
            {state.loading ? "Loading…" : "Load more conversation"}
          </Button>
        ) : null}
        {recentRuns.length > 0 ? (
          <ControlStrip
            hint={`${recentRuns.length} other ${recentRuns.length === 1 ? "task" : "tasks"}`}
            label="Recent Agent tasks"
            summary="RECENT TASKS"
          >
            <Button
              disabled={state.loading || state.submitting}
              onPress={() => setRecentRunsExpanded((value) => !value)}
            >
              {recentRunsExpanded ? "Hide recent tasks" : `Show ${recentRuns.length} recent tasks`}
            </Button>
            {recentRunsExpanded
              ? recentRuns.map((run) => (
                  <Button
                    disabled={state.loading || state.submitting}
                    key={run.id}
                    onPress={() => void selectRun(run.id)}
                  >
                    {runLabel(run)}
                  </Button>
                ))
              : null}
          </ControlStrip>
        ) : null}
        {state.selected && (state.turns.length > 1 || state.turnNextBefore) ? (
          <>
            <Text tone="eyebrow">TURNS</Text>
            <ControlStrip
              hint={`${state.turns.length} loaded`}
              label="Agent task Turns"
              summary={
                state.selectedTurn
                  ? `Turn ${state.selectedTurn.generation} · ${state.selectedTurn.status}`
                  : "Choose a Turn"
              }
            >
              {state.turns.map((turn) => (
                <Button
                  disabled={state.loading || turn.id === state.selectedTurn?.id}
                  key={turn.id}
                  onPress={() => void selectTurn(turn)}
                >
                  Turn {turn.generation} · {turn.status}
                </Button>
              ))}
            </ControlStrip>
          </>
        ) : null}
        {state.turnNextBefore ? (
          <Button disabled={state.loading} onPress={() => void loadOlderTurns()}>
            {state.loading ? "Loading…" : "Load older Turns"}
          </Button>
        ) : null}
        {state.selected && state.selectedTurn ? (
          <>
            <ControlStrip
              hint={`${state.receipts.length} recorded`}
              label={`Command receipts for Turn ${state.selectedTurn.generation}`}
              summary={`COMMAND RECEIPTS · TURN ${state.selectedTurn.generation}`}
            >
              {state.receipts.length === 0 ? (
                <Status state={active ? "pending" : "ready"}>
                  {active ? "Waiting for the first command receipt." : "No command receipts for this Turn."}
                </Status>
              ) : (
                <Button disabled={state.loading} onPress={() => setReceiptsExpanded((value) => !value)}>
                  {receiptsExpanded
                    ? "Hide receipts"
                    : `Show ${state.receipts.length} ${state.receipts.length === 1 ? "receipt" : "receipts"}`}
                </Button>
              )}
            </ControlStrip>
            {receiptsExpanded
              ? state.receipts.map((receipt) => (
                  <ResourceCard
                    actions={
                      onFocusReceiptRef && receipt.resultRefs.length > 0
                        ? receipt.resultRefs.map((ref, index) => (
                            <Button
                              key={`${receipt.id}:focus:${index}`}
                              onPress={() => {
                                const focusNotice = onFocusReceiptRef(ref);
                                setState((current) => ({ ...current, focusNotice }));
                              }}
                            >
                              Focus {receiptRefLabel(ref)}
                            </Button>
                          ))
                        : undefined
                    }
                    details={receiptDetails(receipt)}
                    eyebrow={`${receipt.class.toUpperCase()} · #${receipt.ordinal}`}
                    key={receipt.id}
                    status={<Status state={receiptStatusState(receipt)}>{receiptStatusLabel(receipt)}</Status>}
                    title={receipt.command}
                  />
                ))
              : null}
            {receiptsExpanded && state.receiptNextAfter ? (
              <Button disabled={state.loading} onPress={() => void loadMoreReceipts()}>
                {state.loading ? "Loading…" : "Load more receipts"}
              </Button>
            ) : null}
          </>
        ) : null}
        {state.focusNotice ? <Status state="ready">{state.focusNotice}</Status> : null}
        {state.error ? <Status state="unavailable">{state.error.message}</Status> : null}
      </Stack>
    </PanelDock>
  );
}

function selectedAttachments(
  candidates: readonly CreatorAgentContextCandidate[],
  keys: readonly string[],
): readonly AgentContextAttachment[] {
  const selected = new Set(keys);
  return candidates.filter((candidate) => selected.has(candidate.key)).map((candidate) => candidate.attachment);
}

function messageRole(message: AgentConversationMessage): string {
  if (message.role === "creator") return "YOU";
  if (message.role === "agent") return "AGENT";
  return "SYSTEM";
}

function messageTitle(message: AgentConversationMessage): string {
  if (message.role === "creator") return "Your request";
  if (message.role === "agent") return "Agent response";
  return "Context recovery";
}

function runStatusState(run: AgentRun): "ready" | "pending" | "unavailable" {
  if (run.status === "authorizing" || run.status === "active" || run.status === "waiting") return "pending";
  if (run.status === "failed" || run.status === "cancelled") return "unavailable";
  return "ready";
}

function runStatusLabel(run: AgentRun): string {
  if (run.status === "authorizing") return "Starting";
  if (run.status === "active") return "Working";
  if (run.status === "waiting") return "Waiting";
  if (run.status === "paused") return run.waitingReason === "awaiting-creator" ? "Ready for you" : "Paused";
  if (run.status === "completed") return "Completed";
  if (run.status === "failed") return "Failed";
  return "Cancelled";
}

function receiptStatusState(receipt: CommandReceipt): "ready" | "pending" | "unavailable" {
  if (receipt.status === "succeeded") return "ready";
  if (receipt.status === "accepted" || receipt.status === "waiting" || receipt.status === "approval-required") {
    return "pending";
  }
  return "unavailable";
}

function receiptStatusLabel(receipt: CommandReceipt): string {
  if (receipt.status === "approval-required") return "Approval required";
  return `${receipt.status[0]?.toUpperCase() ?? ""}${receipt.status.slice(1)}`;
}

function outcomeTitle(receipt: CommandReceipt): string {
  if (receipt.status === "succeeded" && receipt.projectRevision) return "Creative change committed";
  if (receipt.status === "approval-required") return "Change needs approval";
  if (receipt.status === "conflict") return "Change needs replanning";
  if (receipt.status === "accepted" || receipt.status === "waiting") return "Command accepted";
  if (receipt.status === "succeeded") return "Command succeeded";
  return "Command did not complete";
}

function outcomeDetails(receipt: CommandReceipt): readonly string[] {
  const detail = outcomeProjection(receipt) ?? receipt.command;
  return [receipt.projectRevision ? `${detail} · Project r${receipt.projectRevision}` : detail];
}

function outcomeProjection(receipt: CommandReceipt): string | undefined {
  const surfaces: string[] = [];
  const addSurface = (surface: string) => !surfaces.includes(surface) && surfaces.push(surface);
  for (const ref of receipt.resultRefs) {
    if (ref.kind === "narrative-document" || ref.kind === "narrative-node") addSurface("Story");
    else if (ref.kind === "clip" || ref.kind === "sequence" || ref.kind === "track") addSurface("Timeline");
    else if (ref.kind === "caption") addSurface("Captions");
    else if (ref.kind === "asset" || ref.kind === "asset-media-state") addSurface("Media");
    else if (ref.kind === "export-artifact") addSurface("Export");
  }
  return surfaces.length > 0 ? `${surfaces.join(" + ")} updated` : undefined;
}

function receiptDetails(receipt: CommandReceipt): readonly string[] {
  const details: string[] = [];
  if (receipt.projectRevision) details.push(`Project r${receipt.projectRevision}`);
  if (receipt.activityCursor) details.push(`Activity #${receipt.activityCursor}`);
  for (const ref of receipt.resultRefs) {
    details.push(`${receiptRefLabel(ref)}${ref.revision ? ` · r${ref.revision}` : ""}`);
  }
  return details;
}

function receiptRefLabel(ref: CommandReceiptRef): string {
  return ref.kind
    .split(/[-_]/)
    .filter(Boolean)
    .map((part) => `${part.slice(0, 1).toUpperCase()}${part.slice(1)}`)
    .join(" ");
}

function attachmentLabel(attachment: AgentContextAttachment): string {
  if ("entity" in attachment) return `${attachment.kind} · ${attachment.entity.id} · r${attachment.entity.revision}`;
  if ("transcript" in attachment) {
    return `transcript segment · ${attachment.transcript.segmentId}`;
  }
  if ("point" in attachment) return `sequence point · ${attachment.point.time.value}/${attachment.point.time.scale}`;
  return `sequence range · ${attachment.range.range.start.value}/${attachment.range.range.start.scale}`;
}

function requestID(action: string): string {
  return `ui:agent-${action}:${crypto.randomUUID()}`;
}

function asError(value: unknown): Error {
  return value instanceof Error ? value : new Error(String(value));
}

function availabilityText(value: AgentAvailability | undefined): string {
  if (!value) return "Checking local Agent availability…";
  if (value.state === "available") return `Ready · ${value.version}`;
  if (value.state === "missing") return "A qualified Codex CLI was not found.";
  if (value.state === "unauthenticated") return "Codex needs an OS keyring-backed sign-in.";
  return "The local Agent or stable Open Cut CLI is incompatible.";
}

function runLabel(run: AgentRun): string {
  const intent = run.intent.length > 28 ? `${run.intent.slice(0, 27)}…` : run.intent;
  return `T${run.currentTurn.generation} · ${run.status.toUpperCase()} · ${intent}`;
}

function presentationText(value: AgentPresentation): string {
  if (value.kind === "context-rebuilt") return "Rebuilt context from the durable conversation.";
  if (value.kind === "tool-started") return `Working · ${value.tool}`;
  if (value.kind === "tool-completed") return `Finished · ${value.tool}`;
  if (value.kind === "turn-failed") return "The local Agent Turn failed.";
  if (value.kind === "turn-completed") return "Agent response completed.";
  return "Local Agent is working…";
}
