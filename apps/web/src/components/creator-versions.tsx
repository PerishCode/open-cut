import { Button, ControlList, ControlStrip, EmptyState, Stack, Status, Text } from "@open-cut/components";
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
  | Readonly<{ status: "unavailable" }>;

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
  const [restoring, setRestoring] = useState(false);
  const [restoreCandidate, setRestoreCandidate] = useState<DurableID>();
  const [notice, setNotice] = useState(undefined as string | undefined);
  const [actionError, setActionError] = useState(undefined as string | undefined);
  const loadGeneration = useRef(0);

  const load = useCallback(
    async (signal?: AbortSignal) => {
      const generation = ++loadGeneration.current;
      try {
        const page = await contracts.projects.versions.list(projectId, { limit: 20 }, signal);
        if (!signal?.aborted && generation === loadGeneration.current) {
          setState({ status: "ready", page, loadingOlder: false });
        }
      } catch {
        if (!signal?.aborted && generation === loadGeneration.current) {
          setState({ status: "unavailable" });
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
    } catch {
      setActionError("Could not load older project versions. Try again.");
      setState(current);
    }
  }, [contracts.projects.versions, projectId, state]);

  const restore = useCallback(
    async (version: ProjectVersion) => {
      if (restoring || version.capturedProjectRevision === currentRevision) return;
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
      } catch {
        setActionError(
          committed
            ? "The version was restored, but the workspace could not refresh. Choose Sync now to reload it."
            : "Could not restore this project version. Review it and try again.",
        );
      } finally {
        setRestoring(false);
      }
    },
    [contracts.projects.versions, currentRevision, load, onRestored, projectId, restoring],
  );

  return (
    <Stack spacing="compact">
      {notice ? <Status state="ready">{notice}</Status> : null}
      {actionError ? <Status state="unavailable">{actionError}</Status> : null}
      {state.status === "loading" ? <Text>Loading project versions…</Text> : null}
      {state.status === "unavailable" ? (
        <Stack spacing="compact">
          <Status state="unavailable">Could not load project versions.</Status>
          <Button onPress={() => void load()}>Try again</Button>
        </Stack>
      ) : null}
      {state.status === "ready" && state.page.versions.length === 0 ? (
        <EmptyState hint="A checkpoint will be created before the next Agent turn." title="No versions yet" />
      ) : null}
      {state.status === "ready" && state.page.versions.length > 0 ? (
        <ControlList label="Recent project checkpoints">
          {state.page.versions.map((version) => {
            const current = version.capturedProjectRevision === currentRevision;
            const confirming = restoreCandidate === version.id;
            const title = version.name ?? versionSourceLabel(version);
            return (
              <ControlStrip
                action={
                  confirming ? undefined : current ? (
                    <Status state="ready">Current</Status>
                  ) : (
                    <Button
                      disabled={restoring}
                      label={`Review restore ${title} at r${version.capturedProjectRevision}`}
                      onPress={() => {
                        setActionError(undefined);
                        setNotice(undefined);
                        setRestoreCandidate(version.id);
                      }}
                    >
                      Review restore
                    </Button>
                  )
                }
                hint={`${formatByteSize(version.byteSize)} · ${formatTimestamp(version.createdAt)}`}
                key={version.id}
                label={`Project version ${title} at revision ${version.capturedProjectRevision}`}
                summary={`${current ? "CURRENT · " : ""}${
                  version.source === "manual" ? "NAMED" : "AUTO"
                } · r${version.capturedProjectRevision} · ${title}`}
              >
                {confirming ? (
                  <>
                    <Status state="pending">
                      Restore creates a new revision. Your current state is checkpointed automatically first.
                    </Status>
                    <Button disabled={restoring} onPress={() => void restore(version)}>
                      {restoring ? "Restoring version…" : "Restore as new revision"}
                    </Button>
                    <Button disabled={restoring} onPress={() => setRestoreCandidate(undefined)}>
                      Cancel
                    </Button>
                  </>
                ) : null}
              </ControlStrip>
            );
          })}
        </ControlList>
      ) : null}
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
