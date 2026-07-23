import type { DurableID, RevisionString } from "@open-cut/contracts";
import { useCallback, useState } from "react";

export type NarrativeInsertionAnchor = Readonly<{
  parentId: DurableID;
  parentRevision: RevisionString;
  afterNodeId?: DurableID;
  label: string;
}>;

type NarrativeSectionPath = readonly DurableID[];

export function useNarrativeHandoff() {
  const [anchor, setAnchor] = useState<NarrativeInsertionAnchor>();
  const [sectionPath, setSectionPath] = useState<NarrativeSectionPath>([]);
  const [recentlyAddedNodeId, setRecentlyAddedNodeId] = useState<DurableID>();
  const [selectionEpoch, setSelectionEpoch] = useState(0);

  const reset = useCallback(() => {
    setAnchor(undefined);
    setSectionPath([]);
    setRecentlyAddedNodeId(undefined);
    setSelectionEpoch((current) => current + 1);
  }, []);
  const select = useCallback((nextAnchor: NarrativeInsertionAnchor, nextSectionPath: NarrativeSectionPath) => {
    setAnchor(nextAnchor);
    setSectionPath(nextSectionPath);
    setRecentlyAddedNodeId(undefined);
    setSelectionEpoch((current) => current + 1);
  }, []);
  const reveal = useCallback((insertedAnchor: NarrativeInsertionAnchor) => {
    setAnchor(insertedAnchor);
    setRecentlyAddedNodeId(insertedAnchor.afterNodeId);
    setSelectionEpoch((current) => current + 1);
  }, []);

  return { anchor, recentlyAddedNodeId, reset, reveal, sectionPath, select, selectionEpoch };
}
