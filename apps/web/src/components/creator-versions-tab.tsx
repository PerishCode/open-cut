import { Stack, type TabDefinition, Text } from "@open-cut/components";
import type { DurableID, ProjectVersionRestored, RevisionString } from "@open-cut/contracts";

import { CreatorHistory } from "./creator-history.js";
import { CreatorVersionCheckpoint } from "./creator-version-checkpoint.js";
import { CreatorVersions } from "./creator-versions.js";

export function creatorVersionsTab({
  currentRevision,
  onCheckpointSaved,
  onRestored,
  projectId,
  refreshEpoch,
}: Readonly<{
  currentRevision: RevisionString | undefined;
  onCheckpointSaved(): void;
  onRestored(result: ProjectVersionRestored): unknown;
  projectId: DurableID;
  refreshEpoch: number;
}>): TabDefinition {
  return {
    id: "versions",
    label: "Versions",
    header: currentRevision ? (
      <CreatorVersionCheckpoint onSaved={onCheckpointSaved} projectId={projectId} />
    ) : undefined,
    content: currentRevision ? (
      <Stack spacing="compact">
        <CreatorVersions
          currentRevision={currentRevision}
          onRestored={onRestored}
          projectId={projectId}
          refreshEpoch={refreshEpoch}
        />
        <CreatorHistory projectId={projectId} refreshEpoch={refreshEpoch} />
      </Stack>
    ) : (
      <Text>Synchronizing project versions…</Text>
    ),
  };
}
