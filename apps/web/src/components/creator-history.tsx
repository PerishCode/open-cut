import { Button, Stack, Status, Text } from "@open-cut/components";
import { type CreatorHistoryPage, type DurableID, useContracts } from "@open-cut/contracts";
import { useCallback, useEffect, useRef, useState } from "react";

type HistoryState =
  | Readonly<{ status: "loading" }>
  | Readonly<{ status: "ready"; page: CreatorHistoryPage; loadingOlder: boolean }>
  | Readonly<{ status: "unavailable"; error: Error }>;

export function CreatorHistory({
  projectId,
  refreshEpoch,
}: Readonly<{
  projectId: DurableID;
  refreshEpoch: number;
}>) {
  const contracts = useContracts();
  const [state, setState] = useState<HistoryState>({ status: "loading" });
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

  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">TRANSACTION LOG · TECHNICAL DETAIL</Text>
      {state.status === "loading" ? <Text>Loading recent creative transactions…</Text> : null}
      {state.status === "unavailable" ? (
        <Stack spacing="compact">
          <Status state="unavailable">History unavailable · {state.error.message}</Status>
          <Button onPress={() => void load()}>Retry history</Button>
        </Stack>
      ) : null}
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
