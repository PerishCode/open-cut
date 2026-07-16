import { Button, Stack, Status, Text } from "@open-cut/components";
import { type CreatorEditCommit, type CreatorHistoryPage, type DurableID, useContracts } from "@open-cut/contracts";
import { useCallback, useEffect, useRef, useState } from "react";

type AsyncResult = unknown;
type HistoryState =
  | Readonly<{ status: "loading" }>
  | Readonly<{ status: "ready"; page: CreatorHistoryPage; loadingOlder: boolean }>
  | Readonly<{ status: "unavailable"; error: Error }>;

export function CreatorHistory({
  onCommitted,
  projectId,
  refreshEpoch,
  sequenceId,
}: Readonly<{
  onCommitted(receipt: CreatorEditCommit): Promise<AsyncResult>;
  projectId: DurableID;
  refreshEpoch: number;
  sequenceId: DurableID;
}>) {
  const contracts = useContracts();
  const [state, setState] = useState<HistoryState>({ status: "loading" });
  const [undoing, setUndoing] = useState(false);
  const [undoError, setUndoError] = useState<Error>();
  const loadGeneration = useRef(0);

  const load = useCallback(
    async (signal?: AbortSignal) => {
      const generation = ++loadGeneration.current;
      setState({ status: "loading" });
      try {
        const page = await contracts.editing.history.list({ projectId, limit: 20 }, signal);
        if (!signal?.aborted && generation === loadGeneration.current) {
          setState({ status: "ready", page, loadingOlder: false });
        }
      } catch (value) {
        if (!signal?.aborted && generation === loadGeneration.current) {
          setState({ status: "unavailable", error: asError(value) });
        }
      }
    },
    [contracts.editing.history, projectId],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load, refreshEpoch]);

  const loadOlder = useCallback(async () => {
    if (state.status !== "ready" || !state.page.nextBefore || state.loadingOlder) return;
    const current = state;
    setState({ ...current, loadingOlder: true });
    try {
      const older = await contracts.editing.history.list({
        projectId,
        before: current.page.nextBefore,
        limit: 20,
      });
      setState({
        status: "ready",
        page: {
          transactions: [...current.page.transactions, ...older.transactions],
          ...(older.nextBefore ? { nextBefore: older.nextBefore } : {}),
          activityCursor: older.activityCursor,
        },
        loadingOlder: false,
      });
    } catch (value) {
      setState({ status: "unavailable", error: asError(value) });
    }
  }, [contracts.editing.history, projectId, state]);

  const latest = state.status === "ready" ? state.page.transactions[0] : undefined;
  const undoLatest = useCallback(async () => {
    if (!latest || undoing) return;
    setUndoing(true);
    setUndoError(undefined);
    try {
      const receipt = await contracts.editing.write.undo({
        projectId,
        sequenceId,
        transactionId: latest.id,
        requestId: `ui:creator-history-undo:${crypto.randomUUID()}`,
        intent: latest.undoesTransactionId ? "Redo latest undone creative change" : "Undo latest creative change",
      });
      await onCommitted(receipt);
    } catch (value) {
      setUndoError(asError(value));
    } finally {
      setUndoing(false);
    }
  }, [contracts.editing.write, latest, onCommitted, projectId, sequenceId, undoing]);

  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">WORKSPACE HISTORY · DURABLE GLOBAL ORDER</Text>
      {state.status === "loading" ? <Text>Loading recent creative transactions…</Text> : null}
      {state.status === "unavailable" ? (
        <Stack spacing="compact">
          <Status state="unavailable">History unavailable · {state.error.message}</Status>
          <Button onPress={() => void load()}>Retry history</Button>
        </Stack>
      ) : null}
      {latest ? (
        <Button disabled={undoing} onPress={() => void undoLatest()}>
          {undoing
            ? "Committing history transaction…"
            : latest.undoesTransactionId
              ? "Redo previous change"
              : "Undo latest change"}
        </Button>
      ) : null}
      {undoError ? <Status state="unavailable">History action failed · {undoError.message}</Status> : null}
      {state.status === "ready"
        ? state.page.transactions.map((transaction, index) => (
            <Stack key={transaction.id} spacing="compact">
              <Text tone="eyebrow">
                {index === 0 ? "LATEST · " : ""}r{transaction.committedProjectRevision} ·{" "}
                {transaction.actor.toUpperCase()}
                {transaction.undoesTransactionId ? " · UNDO/REDO" : ""}
              </Text>
              <Text>{transaction.intent}</Text>
              <Text>
                {transaction.changes.length} changed entities · {transaction.committedAt}
              </Text>
            </Stack>
          ))
        : null}
      {state.status === "ready" && state.page.transactions.length === 0 ? (
        <Text>No committed creative transactions.</Text>
      ) : null}
      {state.status === "ready" && state.page.nextBefore ? (
        <Button disabled={state.loadingOlder} onPress={() => void loadOlder()}>
          {state.loadingOlder ? "Loading older history…" : "Load older history"}
        </Button>
      ) : null}
    </Stack>
  );
}

function asError(value: unknown): Error {
  return value instanceof Error ? value : new Error(String(value));
}
