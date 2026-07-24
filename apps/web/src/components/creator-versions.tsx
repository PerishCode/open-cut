import { Button, EmptyState, ResourceCard, Stack, Status, Text, TextField } from "@open-cut/components";
import {
  type DurableID,
  type ProjectVersion,
  type ProjectVersionPage,
  type ProjectVersionRestored,
  type RevisionString,
  useContracts,
} from "@open-cut/contracts";
import { useCallback, useEffect, useRef, useState } from "react";

type VersionState =
  | Readonly<{ status: "loading" }>
  | Readonly<{ status: "ready"; page: ProjectVersionPage; loadingOlder: boolean }>
  | Readonly<{ status: "unavailable"; error: Error }>;

export function CreatorVersions({
  currentRevision,
  onRestored,
  projectId,
  refreshEpoch,
}: Readonly<{
  currentRevision: RevisionString;
  onRestored(result: ProjectVersionRestored): unknown;
  projectId: DurableID;
  refreshEpoch: number;
}>) {
  const contracts = useContracts();
  const [state, setState] = useState<VersionState>({ status: "loading" });
  const [name, setName] = useState("");
  const [saving, setSaving] = useState(false);
  const [restoring, setRestoring] = useState(false);
  const [restoreCandidate, setRestoreCandidate] = useState<DurableID>();
  const [notice, setNotice] = useState(undefined as string | undefined);
  const [actionError, setActionError] = useState<Error>();
  const loadGeneration = useRef(0);

  const load = useCallback(
    async (signal?: AbortSignal) => {
      const generation = ++loadGeneration.current;
      try {
        const page = await contracts.projects.versions.list(projectId, { limit: 20 }, signal);
        if (!signal?.aborted && generation === loadGeneration.current) {
          setState({ status: "ready", page, loadingOlder: false });
        }
      } catch (value) {
        if (!signal?.aborted && generation === loadGeneration.current) {
          setState({ status: "unavailable", error: asError(value) });
        }
      }
    },
    [contracts.projects.versions, projectId],
  );

  useEffect(() => {
    const controller = new AbortController();
    setState({ status: "loading" });
    void load(controller.signal);
    return () => controller.abort();
  }, [load, refreshEpoch]);

  const loadOlder = useCallback(async () => {
    if (state.status !== "ready" || !state.page.nextBefore || state.loadingOlder) return;
    const current = state;
    setState({ ...current, loadingOlder: true });
    try {
      const older = await contracts.projects.versions.list(projectId, {
        before: current.page.nextBefore,
        limit: 20,
      });
      setState({
        status: "ready",
        page: {
          versions: [...current.page.versions, ...older.versions],
          ...(older.nextBefore === undefined ? {} : { nextBefore: older.nextBefore }),
          activityCursor: older.activityCursor,
        },
        loadingOlder: false,
      });
    } catch (value) {
      setActionError(asError(value));
      setState(current);
    }
  }, [contracts.projects.versions, projectId, state]);

  const save = useCallback(async () => {
    const normalizedName = name.trim();
    if (!normalizedName || saving || restoring) return;
    setSaving(true);
    setActionError(undefined);
    setNotice(undefined);
    try {
      const result = await contracts.projects.versions.create({
        projectId,
        requestId: `ui:project-version-create:${crypto.randomUUID()}`,
        name: normalizedName,
      });
      setName("");
      setNotice(`Saved “${result.version.name ?? normalizedName}” at r${result.version.capturedProjectRevision}.`);
      await load();
    } catch (value) {
      setActionError(asError(value));
    } finally {
      setSaving(false);
    }
  }, [contracts.projects.versions, load, name, projectId, restoring, saving]);

  const restore = useCallback(
    async (version: ProjectVersion) => {
      if (restoring || saving || version.capturedProjectRevision === currentRevision) return;
      let committed = false;
      setRestoring(true);
      setActionError(undefined);
      setNotice(undefined);
      try {
        const result = await contracts.projects.versions.restore({
          projectId,
          versionId: version.id,
          requestId: `ui:project-version-restore:${crypto.randomUUID()}`,
          expectedProjectRevision: currentRevision,
        });
        committed = true;
        setRestoreCandidate(undefined);
        setNotice(
          `Restored “${version.name ?? versionSourceLabel(version)}” as r${result.committedProjectRevision}. ` +
            `The previous state is saved at r${result.safetyVersion.capturedProjectRevision}.`,
        );
        await onRestored(result);
        await load();
      } catch (value) {
        const error = asError(value);
        setActionError(
          committed ? new Error(`Restore committed, but the workspace refresh failed: ${error.message}`) : error,
        );
      } finally {
        setRestoring(false);
      }
    },
    [contracts.projects.versions, currentRevision, load, onRestored, projectId, restoring, saving],
  );

  return (
    <Stack spacing="compact">
      <Text tone="eyebrow">PROJECT VERSIONS · RECOVERY CHECKPOINTS</Text>
      <Text>Lightweight before Agent turns · named versions never copy Source media.</Text>
      <TextField
        disabled={saving || restoring}
        label="Version name"
        maxLength={200}
        onChange={setName}
        onKeyDown={(event) => {
          if (event.key === "Enter") void save();
        }}
        placeholder="e.g. Approved assembly"
        value={name}
      />
      <Button disabled={!name.trim() || saving || restoring} variant="primary" onPress={() => void save()}>
        {saving ? "Saving version…" : "Save version"}
      </Button>
      {notice ? <Status state="ready">{notice}</Status> : null}
      {actionError ? <Status state="unavailable">Version action failed · {actionError.message}</Status> : null}
      {state.status === "loading" ? <Text>Loading project versions…</Text> : null}
      {state.status === "unavailable" ? (
        <Stack spacing="compact">
          <Status state="unavailable">Versions unavailable · {state.error.message}</Status>
          <Button onPress={() => void load()}>Retry versions</Button>
        </Stack>
      ) : null}
      {state.status === "ready" && state.page.versions.length === 0 ? (
        <EmptyState hint="A checkpoint will be created before the next Agent turn." title="No versions yet" />
      ) : null}
      {state.status === "ready"
        ? state.page.versions.map((version) => {
            const current = version.capturedProjectRevision === currentRevision;
            const confirming = restoreCandidate === version.id;
            return (
              <ResourceCard
                actions={
                  confirming ? (
                    <Stack spacing="compact">
                      <Status state="pending">
                        Restore creates a new revision. Your current state is checkpointed automatically first.
                      </Status>
                      <Button disabled={restoring} onPress={() => void restore(version)}>
                        {restoring ? "Restoring version…" : "Restore as new revision"}
                      </Button>
                      <Button disabled={restoring} onPress={() => setRestoreCandidate(undefined)}>
                        Cancel
                      </Button>
                    </Stack>
                  ) : (
                    <Button
                      disabled={current || restoring || saving}
                      onPress={() => {
                        setActionError(undefined);
                        setNotice(undefined);
                        setRestoreCandidate(version.id);
                      }}
                    >
                      {current ? "Current version" : "Review restore"}
                    </Button>
                  )
                }
                details={[
                  `${versionSourceLabel(version)} · ${formatByteSize(version.byteSize)} · ${formatTimestamp(version.createdAt)}`,
                ]}
                eyebrow={`${version.source === "manual" ? "NAMED" : "AUTO"} · r${version.capturedProjectRevision}`}
                key={version.id}
                status={current ? <Status state="ready">Current</Status> : undefined}
                title={version.name ?? versionSourceLabel(version)}
              />
            );
          })
        : null}
      {state.status === "ready" && state.page.nextBefore ? (
        <Button disabled={state.loadingOlder} onPress={() => void loadOlder()}>
          {state.loadingOlder ? "Loading older versions…" : "Load older versions"}
        </Button>
      ) : null}
    </Stack>
  );
}

function versionSourceLabel(version: ProjectVersion): string {
  switch (version.source) {
    case "genesis":
      return "Project created";
    case "agent-turn":
      return "Before Agent turn";
    case "pre-restore":
      return "Before restore";
    case "manual":
      return "Named checkpoint";
  }
}

function formatTimestamp(value: string): string {
  return `${new Date(value).toISOString().slice(0, 16).replace("T", " ")} UTC`;
}

function formatByteSize(value: string): string {
  const bytes = BigInt(value);
  if (bytes < 1024n) return `${bytes} B`;
  if (bytes < 1024n * 1024n) return `${Number(bytes / 1024n)} KiB`;
  return `${Number(bytes / (1024n * 1024n))} MiB`;
}

function asError(value: unknown): Error {
  return value instanceof Error ? value : new Error(String(value));
}
