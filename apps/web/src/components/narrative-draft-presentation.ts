type NarrativeDraftPhase = "clean" | "dirty" | "saving" | "saving-dirty" | "conflict" | "error";

export function narrativeDraftStatusState(phase: NarrativeDraftPhase): "ready" | "pending" | "unavailable" {
  if (phase === "clean") return "ready";
  if (phase === "conflict" || phase === "error") return "unavailable";
  return "pending";
}

export function narrativeDraftStatusText(phase: NarrativeDraftPhase): string {
  if (phase === "clean") return "Committed";
  if (phase === "dirty") return "Unsaved · checkpoints after 750 ms";
  if (phase === "saving") return "Saving checkpoint…";
  if (phase === "saving-dirty") return "Saving older checkpoint · newer text remains unsaved";
  if (phase === "conflict") return "Conflict · local text preserved";
  return "Checkpoint failed · local text preserved";
}
