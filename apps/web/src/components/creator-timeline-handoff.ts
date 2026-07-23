import type { CreatorEditCommit, DurableID } from "@open-cut/contracts";
import { useCallback, useState } from "react";

export type CreatorTimelineHandoff = Readonly<{
  clipIds: readonly DurableID[];
  notice: string;
}>;

export function useCreatorTimelineHandoff() {
  const [current, setCurrent] = useState<CreatorTimelineHandoff>();
  const clear = useCallback(() => setCurrent(undefined), []);
  const revealRoughCut = useCallback((receipt: CreatorEditCommit, projectionReady: boolean) => {
    const clipIds = receipt.allocation
      .filter((allocation) => allocation.kind === "clip")
      .map((allocation) => allocation.id);
    const count = clipIds.length;
    setCurrent({
      clipIds,
      notice: projectionReady
        ? `Rough cut added · ${count} ${count === 1 ? "clip" : "clips"} highlighted`
        : "Rough cut added · refresh reads to reveal it",
    });
  }, []);
  return { clear, current, reset: clear, revealRoughCut };
}
