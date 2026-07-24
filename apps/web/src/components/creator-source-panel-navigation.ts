import type { Asset, DurableID } from "@open-cut/contracts";

import type { TranscriptState } from "./creator-workspace-media.js";

export function createSourcePanelNavigation(
  selectPanel: (panelId: string) => void,
  readTranscript: (assetId: DurableID) => unknown,
  selectedAsset: Asset | undefined,
  transcript: TranscriptState,
): (panelId: string) => void {
  return (panelId) => {
    selectPanel(panelId);
    if (
      panelId === "source-transcript" &&
      selectedAsset &&
      (transcript.status === "idle" || transcript.assetId !== selectedAsset.id) &&
      selectedAsset.artifacts.some((artifact) => artifact.kind === "transcript" && artifact.state === "ready")
    ) {
      readTranscript(selectedAsset.id);
    }
  };
}
