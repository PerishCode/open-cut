import type { DurableID, RevisionString } from "@open-cut/contracts";

export type NarrativeInsertionAnchor = Readonly<{
  parentId: DurableID;
  parentRevision: RevisionString;
  afterNodeId?: DurableID;
  label: string;
}>;
