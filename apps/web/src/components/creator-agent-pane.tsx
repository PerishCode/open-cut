import { Button, PanelDock, Stack, Text, TextAreaField } from "@open-cut/components";
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
import { useCallback, useEffect, useState } from "react";

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

  return (
    <PanelDock
      footer={
        <Stack spacing="compact">
          {onAddTimelineContext || onAddPlayheadContext ? <Text tone="eyebrow">QUICK CONTEXT</Text> : null}
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
          {contextCandidates.length > 0 ? <Text tone="eyebrow">@ CONTEXT</Text> : null}
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
          <TextAreaField
            disabled={!canSubmit}
            label={state.selected ? "Continue this task" : "New task"}
            maxLength={8000}
            placeholder={
              canContinue || !state.selected
                ? "Tell the Agent what to write or change…"
                : "Wait for this Turn to finish."
            }
            rows={5}
            value={message}
            onChange={setMessage}
          />
          <Button disabled={!canSubmit || message.trim() === ""} onPress={() => void submit()}>
            {state.submitting ? "Submitting…" : state.selected ? "Continue" : "Start task"}
          </Button>
        </Stack>
      }
      header={
        <Stack spacing="compact">
          <Text tone="eyebrow">LOCAL AGENT</Text>
          <Text>{availabilityText(state.availability)}</Text>
          <Button disabled={state.loading || state.submitting} onPress={() => void load()}>
            Refresh Agent
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
            }}
          >
            New task
          </Button>
        </Stack>
      }
      label="Agent collaboration"
    >
      <Stack spacing="compact">
        {state.runs.length > 0 ? <Text tone="eyebrow">RECENT TASKS</Text> : null}
        {state.runs.map((run) => (
          <Button
            disabled={state.loading || state.submitting || run.id === state.selected?.id}
            key={run.id}
            onPress={() => void selectRun(run.id)}
          >
            {runLabel(run)}
          </Button>
        ))}
        {state.selected ? (
          <Text tone="eyebrow">
            {state.selected.status.toUpperCase()} · TURN {state.selected.currentTurn.generation}
            {state.selected.waitingReason ? ` · ${state.selected.waitingReason}` : ""}
          </Text>
        ) : (
          <Text>Describe a new writing or editing task.</Text>
        )}
        {state.messages.map((entry) => (
          <Stack key={entry.id} spacing="compact">
            <Text tone="eyebrow">{entry.role === "creator" ? "YOU" : entry.role === "agent" ? "AGENT" : "SYSTEM"}</Text>
            <Text>
              {entry.role === "notice" ? "Agent context was safely rebuilt from this conversation." : entry.text}
            </Text>
            {entry.attachments.map((attachment, index) => (
              <Text key={`${entry.id}:attachment:${index}`}>@ {attachmentLabel(attachment)}</Text>
            ))}
          </Stack>
        ))}
        {state.nextAfter ? (
          <Button disabled={state.loading} onPress={() => void loadMore()}>
            {state.loading ? "Loading…" : "Load more conversation"}
          </Button>
        ) : null}
        {state.selected && state.turns.length > 0 ? <Text tone="eyebrow">TURNS</Text> : null}
        {state.turns.map((turn) => (
          <Button
            disabled={state.loading || turn.id === state.selectedTurn?.id}
            key={turn.id}
            onPress={() => void selectTurn(turn)}
          >
            Turn {turn.generation} · {turn.status}
          </Button>
        ))}
        {state.turnNextBefore ? (
          <Button disabled={state.loading} onPress={() => void loadOlderTurns()}>
            {state.loading ? "Loading…" : "Load older Turns"}
          </Button>
        ) : null}
        {state.selected && state.selectedTurn ? (
          <>
            <Text tone="eyebrow">COMMAND RECEIPTS · TURN {state.selectedTurn.generation}</Text>
            {state.receipts.length === 0 ? <Text>No durable command receipts for this Turn yet.</Text> : null}
            {state.receipts.map((receipt) => (
              <Stack key={receipt.id} spacing="compact">
                <Text tone="eyebrow">
                  {receipt.class.toUpperCase()} · {receipt.status.toUpperCase()} · #{receipt.ordinal}
                </Text>
                <Text>{receipt.command}</Text>
                {receipt.resultRefs.map((ref, index) => (
                  <Stack key={`${receipt.id}:${index}`} spacing="compact">
                    <Text>
                      {ref.kind} · {ref.id}
                      {ref.revision ? ` · r${ref.revision}` : ""}
                    </Text>
                    {onFocusReceiptRef ? (
                      <Button
                        onPress={() => setState((current) => ({ ...current, focusNotice: onFocusReceiptRef(ref) }))}
                      >
                        Focus {ref.kind}
                      </Button>
                    ) : null}
                  </Stack>
                ))}
              </Stack>
            ))}
            {state.receiptNextAfter ? (
              <Button disabled={state.loading} onPress={() => void loadMoreReceipts()}>
                {state.loading ? "Loading…" : "Load more receipts"}
              </Button>
            ) : null}
          </>
        ) : null}
        {state.focusNotice ? <Text>{state.focusNotice}</Text> : null}
        {state.presentation ? <Text>{presentationText(state.presentation)}</Text> : null}
        {active ? (
          <>
            <Button disabled={state.submitting} onPress={() => void transition("interrupt")}>
              Stop
            </Button>
            <Button disabled={state.submitting} onPress={() => void transition("cancel")}>
              Cancel task
            </Button>
          </>
        ) : null}
        {!active && state.selected && !terminal ? (
          <Button disabled={state.submitting} onPress={() => void transition("cancel")}>
            Cancel task
          </Button>
        ) : null}
        {terminal ? <Text>This task is closed. Choose New task to start another Run.</Text> : null}
        {state.error ? <Text>{state.error.message}</Text> : null}
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
  const intent = run.intent.length > 48 ? `${run.intent.slice(0, 47)}…` : run.intent;
  return `${run.status.toUpperCase()} · ${intent}`;
}

function presentationText(value: AgentPresentation): string {
  if (value.kind === "context-rebuilt") return "Rebuilt context from the durable conversation.";
  if (value.kind === "tool-started") return `Working · ${value.tool}`;
  if (value.kind === "tool-completed") return `Finished · ${value.tool}`;
  if (value.kind === "turn-failed") return "The local Agent Turn failed.";
  if (value.kind === "turn-completed") return "Agent response completed.";
  return "Local Agent is working…";
}
