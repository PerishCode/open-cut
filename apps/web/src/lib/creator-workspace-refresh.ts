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

export function workspaceStatus(status: "loading" | "unavailable" | "ready"): string {
  if (status === "loading") return "Opening project";
  if (status === "unavailable") return "Project unavailable";
  return "All changes synced";
}
