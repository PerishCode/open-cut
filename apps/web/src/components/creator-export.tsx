import { Button, ControlStrip, EmptyState, ResourceCard, Stack, Status, Text } from "@open-cut/components";
import {
  type DurableID,
  type ExportSaveResult,
  type RevisionString,
  type SequenceExportHistoryPage,
  type SequenceExportLineage,
  useContracts,
} from "@open-cut/contracts";
import { useCallback, useEffect, useMemo, useState } from "react";

type CreatorExportProps = Readonly<{
  projectId: DurableID;
  projectName: string;
  sequenceId: DurableID;
  sequenceRevision: RevisionString;
  available: boolean;
  hasContent: boolean;
}>;

type SavedLineage = Readonly<{
  rootJobId: DurableID;
  result: ExportSaveResult;
}>;

export function CreatorExport({
  projectId,
  projectName,
  sequenceId,
  sequenceRevision,
  available,
  hasContent,
}: CreatorExportProps) {
  const contracts = useContracts();
  const [history, setHistory] = useState<SequenceExportHistoryPage>();
  const [loadingHistory, setLoadingHistory] = useState(true);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<Error>();
  const [save, setSave] = useState<SavedLineage>();
  const [revealedName, setRevealedName] = useState(undefined as string | undefined);
  const [deleteConfirmation, setDeleteConfirmation] = useState<DurableID>();
  const suggestedName = useMemo(() => exportFilename(projectName, sequenceRevision), [projectName, sequenceRevision]);

  const run = useCallback(async <Result,>(operation: () => Promise<Result>): Promise<Result> => {
    setPending(true);
    setError(undefined);
    try {
      return await operation();
    } catch (value) {
      const next = value instanceof Error ? value : new Error(String(value));
      setError(next);
      throw next;
    } finally {
      setPending(false);
    }
  }, []);

  const loadHistory = useCallback(
    async (signal?: AbortSignal) => {
      setLoadingHistory(true);
      try {
        const page = await contracts.exports.list(projectId, { limit: 20 }, signal);
        setHistory(page);
      } catch (value) {
        if (!signal?.aborted) setError(value instanceof Error ? value : new Error(String(value)));
      } finally {
        if (!signal?.aborted) setLoadingHistory(false);
      }
    },
    [contracts, projectId],
  );

  useEffect(() => {
    const controller = new AbortController();
    void loadHistory(controller.signal);
    return () => controller.abort();
  }, [loadHistory]);

  useEffect(
    () => contracts.exports.subscribe(projectId, () => void loadHistory()),
    [contracts, loadHistory, projectId],
  );

  const start = useCallback(
    () =>
      run(async () => {
        setSave(undefined);
        setRevealedName(undefined);
        setDeleteConfirmation(undefined);
        await contracts.exports.start(projectId, sequenceId, {
          requestId: requestIdentity("export-start"),
          sequenceRevision,
          preset: "webm-vp9-opus-v1",
        });
        await loadHistory();
      }),
    [contracts, loadHistory, projectId, run, sequenceId, sequenceRevision],
  );

  const cancel = useCallback(
    (lineage: SequenceExportLineage) =>
      run(async () => {
        await contracts.exports.cancel(projectId, lineage.export.job.id, requestIdentity("export-cancel"));
        await loadHistory();
      }),
    [contracts, loadHistory, projectId, run],
  );

  const retry = useCallback(
    (lineage: SequenceExportLineage) =>
      run(async () => {
        setSave(undefined);
        setDeleteConfirmation(undefined);
        await contracts.exports.retry(projectId, lineage.export.job.id);
        await loadHistory();
      }),
    [contracts, loadHistory, projectId, run],
  );

  const saveAs = useCallback(
    (lineage: SequenceExportLineage, overwrite?: Extract<ExportSaveResult, { status: "overwrite-required" }>) => {
      const artifact = lineage.export.artifact;
      if (!artifact) return undefined;
      return run(async () => {
        const result = await contracts.exports.saveAs({
          projectId,
          artifactId: artifact.id,
          suggestedName: exportFilename(projectName, lineage.export.sequenceRevision),
          ...(overwrite ? { destinationGrant: overwrite.destinationGrant, overwrite: true as const } : {}),
        });
        setSave({ rootJobId: lineage.export.job.rootJobId, result });
        setRevealedName(undefined);
      });
    },
    [contracts, projectId, projectName, run],
  );

  const reveal = useCallback(
    (result: Extract<ExportSaveResult, { status: "saved" }>) =>
      run(async () => {
        const revealed = await contracts.exports.reveal(result.deliveryReceipt);
        setRevealedName(revealed.displayName);
      }),
    [contracts, run],
  );

  const deleteArtifact = useCallback(
    (lineage: SequenceExportLineage) => {
      const artifact = lineage.export.artifact;
      if (!artifact) return undefined;
      return run(async () => {
        await contracts.exports.deleteArtifact(
          projectId,
          lineage.export.job.id,
          artifact.id,
          requestIdentity("export-delete"),
        );
        setDeleteConfirmation(undefined);
        setSave(undefined);
        setRevealedName(undefined);
        await loadHistory();
      });
    },
    [contracts, loadHistory, projectId, run],
  );

  const loadMore = useCallback(
    () =>
      history?.nextAfter &&
      run(async () => {
        const page = await contracts.exports.list(projectId, { after: history.nextAfter, limit: 20 });
        setHistory({
          lineages: [...history.lineages, ...page.lineages],
          ...(page.nextAfter ? { nextAfter: page.nextAfter } : {}),
          activityCursor: page.activityCursor,
        });
      }),
    [contracts, history, projectId, run],
  );

  const active = history?.lineages.some((lineage) => isActive(lineage)) ?? false;
  const nextStatus = !hasContent
    ? { state: "unavailable" as const, label: "Sequence empty" }
    : !available
      ? { state: "unavailable" as const, label: "Unavailable" }
      : active || pending
        ? { state: "pending" as const, label: active ? "Export in progress" : "Working" }
        : { state: "ready" as const, label: "Ready" };
  return (
    <Stack spacing="compact">
      <ControlStrip
        hint={
          hasContent
            ? "DESTINATION AFTER RENDER · WEBM · VP9 / OPUS"
            : "Add a clip or caption to the Sequence before exporting."
        }
        label="Next export"
        summary={`NEXT · SEQUENCE r${sequenceRevision} · ${suggestedName}`}
      >
        <Status state={nextStatus.state}>{nextStatus.label}</Status>
        <Button
          disabled={!available || !hasContent || pending || active}
          variant="primary"
          onPress={() => void start()}
        >
          {!hasContent ? "Nothing to export" : active ? "Export in progress" : "Export current revision"}
        </Button>
      </ControlStrip>
      {loadingHistory && !history ? <Text>Loading exports…</Text> : null}
      {history?.lineages.length === 0 ? (
        <EmptyState hint="Completed exports and retained job history will appear here." title="No exports yet" />
      ) : null}
      {history && history.lineages.length > 0 ? (
        <ResourceCard emphasis="quiet" eyebrow={`${history.lineages.length} LOADED`} title="Recent exports">
          {history.lineages.map((lineage, index) => {
            const rootID = lineage.export.job.rootJobId;
            const filename = exportFilename(projectName, lineage.export.sequenceRevision);
            const historyPosition = index + 1;
            const actionIdentity = `${filename}, history item ${historyPosition}, from ${formatTimestamp(lineage.rootCreatedAt)}`;
            const lineageSave = save?.rootJobId === rootID ? save.result : undefined;
            const overwrite = lineageSave?.status === "overwrite-required" ? lineageSave : undefined;
            const activeLineage = isActive(lineage);
            const availableArtifact = !activeLineage ? lineage.export.artifact : undefined;
            const confirmingDelete = Boolean(availableArtifact && availableArtifact.id === deleteConfirmation);
            const details = exportDetails(lineage);
            return (
              <ControlStrip
                hint={details[0]}
                key={rootID}
                label={`Export ${actionIdentity}`}
                summary={`${index === 0 ? "LATEST" : `HISTORY ${historyPosition}`} · ${lineage.origin.toUpperCase()} · SEQUENCE r${
                  lineage.export.sequenceRevision
                } · ${filename}`}
              >
                <Status state={exportStatusState(lineage)}>{exportStatusLabel(lineage)}</Status>
                {details.slice(1).map((detail) => (
                  <Text key={detail}>{detail}</Text>
                ))}
                {confirmingDelete ? (
                  <Status state="pending">This removes the exported media but keeps its job history.</Status>
                ) : null}
                {activeLineage ? (
                  <Button
                    disabled={pending}
                    label={`Cancel export ${actionIdentity}`}
                    onPress={() => void cancel(lineage)}
                  >
                    Cancel export
                  </Button>
                ) : null}
                {lineage.export.recovery === "retry-job" ? (
                  <Button
                    disabled={!available || pending}
                    label={`Retry export ${actionIdentity}`}
                    onPress={() => void retry(lineage)}
                  >
                    Retry export
                  </Button>
                ) : null}
                {availableArtifact && !confirmingDelete ? (
                  <>
                    <Button
                      disabled={pending}
                      label={`Save export ${actionIdentity} as`}
                      onPress={() => void saveAs(lineage)}
                    >
                      Save As…
                    </Button>
                    <Button
                      disabled={pending}
                      label={`Delete export ${actionIdentity}`}
                      onPress={() => setDeleteConfirmation(availableArtifact.id)}
                    >
                      Delete export…
                    </Button>
                  </>
                ) : null}
                {overwrite ? (
                  <Button
                    disabled={pending}
                    label={`Replace ${overwrite.displayName} with export ${actionIdentity}`}
                    onPress={() => void saveAs(lineage, overwrite)}
                  >
                    Replace {overwrite.displayName}
                  </Button>
                ) : null}
                {confirmingDelete ? (
                  <>
                    <Button
                      disabled={pending}
                      label={`Delete export ${actionIdentity} permanently`}
                      variant="danger"
                      onPress={() => void deleteArtifact(lineage)}
                    >
                      Delete export permanently
                    </Button>
                    <Button
                      disabled={pending}
                      label={`Keep export ${actionIdentity}`}
                      onPress={() => setDeleteConfirmation(undefined)}
                    >
                      Keep export
                    </Button>
                  </>
                ) : null}
                {lineageSave?.status === "saved" ? (
                  <Button
                    disabled={pending}
                    label={`Reveal saved export ${lineageSave.displayName} in folder`}
                    onPress={() => void reveal(lineageSave)}
                  >
                    Reveal in folder
                  </Button>
                ) : null}
                {lineageSave?.status === "saved" ? (
                  <Status state="ready">
                    Saved {lineageSave.displayName} · {lineageSave.byteLength} bytes ·{" "}
                    {lineageSave.contentSha256.slice(0, 19)}…
                  </Status>
                ) : null}
                {lineageSave?.status === "saved" && revealedName === lineageSave.displayName ? (
                  <Text>Revealed {revealedName}</Text>
                ) : null}
              </ControlStrip>
            );
          })}
        </ResourceCard>
      ) : null}
      {history?.nextAfter ? (
        <Button disabled={pending} onPress={() => void loadMore()}>
          Load older exports
        </Button>
      ) : null}
      {error ? <Status state="unavailable">Export action failed · {error.message}</Status> : null}
    </Stack>
  );
}

function isActive(lineage: SequenceExportLineage): boolean {
  const state = lineage.export.job.state;
  return state === "blocked" || state === "queued" || state === "running";
}

function exportStatusState(lineage: SequenceExportLineage): "ready" | "pending" | "unavailable" {
  if (isActive(lineage)) return "pending";
  if (lineage.export.job.state === "succeeded" && lineage.artifactAvailability === "ready") return "ready";
  return "unavailable";
}

function exportStatusLabel(lineage: SequenceExportLineage): string {
  switch (lineage.export.job.state) {
    case "blocked":
      return "Waiting";
    case "queued":
      return "Queued";
    case "running":
      return "Rendering";
    case "failed":
      return "Failed";
    case "cancelled":
      return "Cancelled";
    case "succeeded":
      switch (lineage.artifactAvailability) {
        case "ready":
          return "Ready";
        case "deleted":
          return "Media deleted";
        case "invalid":
          return "Needs repair";
        case "none":
          return "Completed";
      }
  }
}

function exportDetails(lineage: SequenceExportLineage): readonly string[] {
  const value = lineage.export;
  const attempts = `${lineage.attemptCount} attempt${lineage.attemptCount === "1" ? "" : "s"}`;
  const details = [`${value.job.progressBasisPoints / 100}% · ${attempts} · ${formatTimestamp(lineage.rootCreatedAt)}`];
  if (value.artifact) {
    details.push(
      `${value.artifact.canvasWidth} × ${value.artifact.canvasHeight} · ${formatByteSize(value.artifact.byteSize)} · ${value.artifact.videoCodec.toUpperCase()} / ${value.artifact.audioCodec.toUpperCase()}`,
    );
  }
  if (lineage.artifactAvailability === "deleted") details.push("Exported media deleted; durable job history retained.");
  if (value.job.terminalErrorCode) details.push(`Needs attention · ${value.job.terminalErrorCode}`);
  return details;
}

function formatTimestamp(value: string): string {
  return `${new Date(value).toISOString().slice(0, 16).replace("T", " ")} UTC`;
}

function formatByteSize(value: string): string {
  const bytes = BigInt(value);
  if (bytes < 1_024n) return `${bytes} B`;
  if (bytes < 1_024n * 1_024n) return `${bytes / 1_024n} KiB`;
  return `${bytes / (1_024n * 1_024n)} MiB`;
}

function exportFilename(projectName: string, revision: RevisionString): string {
  const stem = projectName
    .normalize("NFKC")
    .replace(/[^\p{L}\p{N}._-]+/gu, "-")
    .replace(/^[._-]+|[._-]+$/g, "")
    .slice(0, 80);
  return `${stem || "open-cut"}-r${revision}.webm`;
}

function requestIdentity(kind: string): string {
  return `ui:${kind}:${crypto.randomUUID()}`;
}
