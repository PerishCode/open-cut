import { Button, Stack, Text } from "@open-cut/components";
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
}>;

type SavedLineage = Readonly<{
  rootJobId: DurableID;
  result: ExportSaveResult;
}>;

export function CreatorExport({ projectId, projectName, sequenceId, sequenceRevision, available }: CreatorExportProps) {
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
  return (
    <Stack spacing="compact">
      <Button disabled={!available || pending || active} onPress={() => void start()}>
        {active ? "Export in progress" : "Export"}
      </Button>
      <Text tone="eyebrow">EXPORT HISTORY</Text>
      {loadingHistory && !history ? <Text>Loading exports…</Text> : null}
      {history?.lineages.length === 0 ? <Text>No exports yet.</Text> : null}
      {history?.lineages.map((lineage) => {
        const rootID = lineage.export.job.rootJobId;
        const lineageSave = save?.rootJobId === rootID ? save.result : undefined;
        const overwrite = lineageSave?.status === "overwrite-required" ? lineageSave : undefined;
        const confirmingDelete = lineage.export.artifact?.id === deleteConfirmation;
        return (
          <Stack key={rootID} spacing="compact">
            <Text tone="eyebrow">{exportStatus(lineage)}</Text>
            {isActive(lineage) ? (
              <Button disabled={pending} onPress={() => void cancel(lineage)}>
                Cancel export
              </Button>
            ) : null}
            {lineage.export.recovery === "retry-job" ? (
              <Button disabled={!available || pending} onPress={() => void retry(lineage)}>
                Retry export
              </Button>
            ) : null}
            {lineage.export.artifact ? (
              <Button disabled={pending} onPress={() => void saveAs(lineage)}>
                Save As…
              </Button>
            ) : null}
            {overwrite ? (
              <Button disabled={pending} onPress={() => void saveAs(lineage, overwrite)}>
                Replace {overwrite.displayName}
              </Button>
            ) : null}
            {lineage.export.artifact && !confirmingDelete ? (
              <Button disabled={pending} onPress={() => setDeleteConfirmation(lineage.export.artifact?.id)}>
                Delete export…
              </Button>
            ) : null}
            {confirmingDelete ? (
              <>
                <Text>This removes the exported media but keeps its job history.</Text>
                <Button disabled={pending} onPress={() => void deleteArtifact(lineage)}>
                  Delete export permanently
                </Button>
                <Button disabled={pending} onPress={() => setDeleteConfirmation(undefined)}>
                  Keep export
                </Button>
              </>
            ) : null}
            {lineageSave?.status === "saved" ? (
              <>
                <Text tone="eyebrow">
                  SAVED {lineageSave.displayName} · {lineageSave.byteLength} BYTES ·{" "}
                  {lineageSave.contentSha256.slice(0, 19)}…
                </Text>
                <Button disabled={pending} onPress={() => void reveal(lineageSave)}>
                  Reveal in folder
                </Button>
                {revealedName === lineageSave.displayName ? <Text>Revealed {revealedName}</Text> : null}
              </>
            ) : null}
          </Stack>
        );
      })}
      {history?.nextAfter ? (
        <Button disabled={pending} onPress={() => void loadMore()}>
          Load older exports
        </Button>
      ) : null}
      {error ? <Text>{error.message}</Text> : null}
      <Text tone="eyebrow">NEXT EXPORT · {suggestedName}</Text>
    </Stack>
  );
}

function isActive(lineage: SequenceExportLineage): boolean {
  const state = lineage.export.job.state;
  return state === "blocked" || state === "queued" || state === "running";
}

function exportStatus(lineage: SequenceExportLineage): string {
  const value = lineage.export;
  const failure = value.job.terminalErrorCode ? ` · ${value.job.terminalErrorCode}` : "";
  const availability =
    lineage.artifactAvailability === "none" ? "" : ` · ${lineage.artifactAvailability.toUpperCase()}`;
  return `EXPORT r${value.sequenceRevision} · ${value.job.state.toUpperCase()} · ${value.job.progressBasisPoints / 100}% · ${lineage.origin.toUpperCase()} · ${lineage.attemptCount} ATTEMPT${availability}${failure}`;
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
