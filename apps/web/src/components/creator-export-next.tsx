import { Button, ControlStrip, Status } from "@open-cut/components";
import { type DurableID, type RevisionString, useContracts } from "@open-cut/contracts";
import { useCallback, useMemo, useState } from "react";

import { exportFilename } from "./creator-export.js";

export function CreatorExportNext({
  activeExport,
  available,
  hasContent,
  onStarted,
  projectId,
  projectName,
  sequenceId,
  sequenceRevision,
}: Readonly<{
  activeExport: boolean;
  available: boolean;
  hasContent: boolean;
  onStarted(): unknown;
  projectId: DurableID;
  projectName: string;
  sequenceId: DurableID;
  sequenceRevision: RevisionString;
}>) {
  const contracts = useContracts();
  const [pending, setPending] = useState(false);
  const [actionError, setActionError] = useState(undefined as string | undefined);
  const suggestedName = useMemo(() => exportFilename(projectName, sequenceRevision), [projectName, sequenceRevision]);

  const start = useCallback(async () => {
    if (pending || activeExport || !available || !hasContent) return;
    setPending(true);
    setActionError(undefined);
    try {
      await contracts.exports.start(projectId, sequenceId, {
        requestId: `ui:export-start:${crypto.randomUUID()}`,
        sequenceRevision,
        preset: "webm-vp9-opus-v1",
      });
      await onStarted();
    } catch {
      setActionError("Could not start this export. Try again.");
    } finally {
      setPending(false);
    }
  }, [
    activeExport,
    available,
    contracts.exports,
    hasContent,
    onStarted,
    pending,
    projectId,
    sequenceId,
    sequenceRevision,
  ]);

  const nextStatus = !hasContent
    ? { state: "unavailable" as const, label: "Sequence empty" }
    : !available
      ? { state: "unavailable" as const, label: "Unavailable" }
      : activeExport || pending
        ? { state: "pending" as const, label: activeExport ? "Export in progress" : "Working" }
        : { state: "ready" as const, label: "Ready" };

  return (
    <>
      <ControlStrip
        hint={
          hasContent
            ? "DESTINATION AFTER RENDER · WEBM · VP9 / OPUS"
            : "Add a clip or caption to the Sequence before exporting."
        }
        label="Next export"
        summary={`NEXT · SEQUENCE r${sequenceRevision} · ${suggestedName}`}
      >
        <Status state={nextStatus.state}>{nextStatus.label}</Status>
        <Button
          disabled={!available || !hasContent || pending || activeExport}
          variant="primary"
          onPress={() => void start()}
        >
          {!hasContent ? "Nothing to export" : activeExport ? "Export in progress" : "Export current revision"}
        </Button>
      </ControlStrip>
      {actionError ? <Status state="unavailable">{actionError}</Status> : null}
    </>
  );
}
