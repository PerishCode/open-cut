import { type TabDefinition, Text } from "@open-cut/components";
import type { DurableID, RevisionString } from "@open-cut/contracts";

import { CreatorExport } from "./creator-export.js";
import { CreatorExportNext } from "./creator-export-next.js";

export function creatorExportTab({
  activeExport,
  available,
  hasContent,
  onActiveChange,
  onStarted,
  projectId,
  projectName,
  refreshEpoch,
  sequence,
}: Readonly<{
  activeExport: boolean;
  available: boolean;
  hasContent: boolean;
  onActiveChange(active: boolean): unknown;
  onStarted(): unknown;
  projectId: DurableID;
  projectName: string;
  refreshEpoch: number;
  sequence: Readonly<{ id: DurableID; revision: RevisionString }> | undefined;
}>): TabDefinition {
  return {
    id: "export",
    label: "Export",
    header: sequence ? (
      <CreatorExportNext
        activeExport={activeExport}
        available={available}
        hasContent={hasContent}
        onStarted={onStarted}
        projectId={projectId}
        projectName={projectName}
        sequenceId={sequence.id}
        sequenceRevision={sequence.revision}
      />
    ) : undefined,
    content: sequence ? (
      <CreatorExport
        available={available}
        onActiveChange={onActiveChange}
        projectId={projectId}
        projectName={projectName}
        refreshEpoch={refreshEpoch}
      />
    ) : (
      <Text>Synchronizing the project…</Text>
    ),
  };
}
