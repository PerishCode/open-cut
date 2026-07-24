import { useCallback, useState } from "react";

type WorkspaceLoad = (signal: AbortSignal | undefined, preserveReady: boolean) => Promise<unknown>;

export type CreatorWorkspaceSync = Readonly<{
  failed: boolean;
  run(): Promise<void>;
  syncing: boolean;
}>;
type WorkspaceStatus = "loading" | "unavailable" | "ready";

export function useCreatorWorkspaceSync(load: WorkspaceLoad, preserveReady: boolean): CreatorWorkspaceSync {
  const [syncing, setSyncing] = useState(false);
  const [failed, setFailed] = useState(false);
  const run = useCallback(async () => {
    if (syncing) return;
    setSyncing(true);
    setFailed(false);
    try {
      await load(undefined, preserveReady);
    } catch {
      setFailed(true);
    } finally {
      setSyncing(false);
    }
  }, [load, preserveReady, syncing]);
  return { failed, run, syncing };
}

export function workspaceSyncStatus(
  status: WorkspaceStatus,
  sync: CreatorWorkspaceSync,
): Readonly<{ state: "pending" | "unavailable" | "ready"; text: string }> {
  if (sync.syncing) return { state: "pending", text: "Syncing project" };
  if (sync.failed) return { state: "unavailable", text: "Could not sync · current view kept" };
  return { state: status === "loading" ? "pending" : status, text: workspaceStatus(status) };
}

/**
 * Background project/media activity reconciliation for a ready Creator workspace.
 *
 * Always uses the preserve-ready load path so product activity cannot blank the
 * editor shell (and unmount Timeline controllers) while a gesture is in flight.
 * Transient failures stay fire-and-forget; explicit post-commit refresh still
 * awaits and throws via the preserve-ready load call site.
 */
export function createBackgroundWorkspaceInvalidation(
  load: (signal: AbortSignal | undefined, preserveReady: boolean) => Promise<unknown>,
): () => Promise<void> {
  return async () => {
    try {
      await load(undefined, true);
    } catch {
      // Keep the previously ready workspace mounted.
    }
  };
}

function workspaceStatus(status: WorkspaceStatus): string {
  if (status === "loading") return "Opening project";
  if (status === "unavailable") return "Project unavailable";
  return "All changes synced";
}
