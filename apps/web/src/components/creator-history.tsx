import { Button, ControlStrip, ResourceCard, Stack, Status, Text } from "@open-cut/components";
import { type CreatorHistoryPage, type DurableID, useContracts } from "@open-cut/contracts";
import { useCallback, useEffect, useRef, useState } from "react";

type HistoryState =
  | Readonly<{ status: "loading" }>
  | Readonly<{ status: "ready"; page: CreatorHistoryPage; loadingOlder: boolean }>
  | Readonly<{ status: "unavailable" }>;

export function CreatorHistory({
  projectId,
  refreshEpoch,
}: Readonly<{
  projectId: DurableID;
  refreshEpoch: number;
}>) {
  const contracts = useContracts();
  const [state, setState] = useState<HistoryState>({ status: "loading" });
  const [loadOlderError, setLoadOlderError] = useState(false);
  const loadGeneration = useRef(0);

  const load = useCallback(
    async (signal?: AbortSignal) => {
      const generation = ++loadGeneration.current;
      setState({ status: "loading" });
      setLoadOlderError(false);
      try {
        const page = await contracts.editing.history.list({ projectId, limit: 20 }, signal);
        if (!signal?.aborted && generation === loadGeneration.current) {
          setState({ status: "ready", page, loadingOlder: false });
        }
      } catch {
        if (!signal?.aborted && generation === loadGeneration.current) {
          setState({ status: "unavailable" });
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
    setLoadOlderError(false);
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
    } catch {
      setState(current);
      setLoadOlderError(true);
    }
  }, [contracts.editing.history, projectId, state]);

  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">TRANSACTION LOG · TECHNICAL DETAIL</Text>
      {state.status === "loading" ? <Text>Loading recent creative transactions…</Text> : null}
      {state.status === "unavailable" ? (
        <Stack spacing="compact">
          <Status state="unavailable">Could not load project history.</Status>
          <Button onPress={() => void load()}>Try again</Button>
        </Stack>
      ) : null}
      {loadOlderError ? <Status state="unavailable">Could not load older history. Try again.</Status> : null}
      {state.status === "ready" && state.page.transactions.length > 0 ? (
        <ResourceCard
          emphasis="quiet"
          eyebrow={`${state.page.transactions.length} LOADED`}
          title="Recent creative transactions"
        >
          {state.page.transactions.map((transaction, index) => (
            <ControlStrip
              hint={`${formatChangeCount(transaction.changes.length)} · ${formatTimestamp(transaction.committedAt)}`}
              key={transaction.id}
              label={`Transaction r${transaction.committedProjectRevision}: ${transaction.intent}`}
              summary={`${index === 0 ? "LATEST · " : ""}r${transaction.committedProjectRevision} · ${transaction.actor.toUpperCase()}${
                transaction.undoesTransactionId ? " · UNDO/REDO" : ""
              } · ${transaction.intent}`}
            />
          ))}
        </ResourceCard>
      ) : null}
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

function formatChangeCount(count: number): string {
  return `${count} ${count === 1 ? "CHANGE" : "CHANGES"}`;
}

function formatTimestamp(value: string): string {
  return `${new Date(value).toISOString().slice(0, 16).replace("T", " ")} UTC`;
}
