import { Button, ControlStrip, Status, TextField } from "@open-cut/components";
import { type DurableID, type ProjectVersionCreated, useContracts } from "@open-cut/contracts";
import { useCallback, useState } from "react";

export function CreatorVersionCheckpoint({
  onSaved,
  projectId,
}: Readonly<{
  onSaved(result: ProjectVersionCreated): unknown;
  projectId: DurableID;
}>) {
  const contracts = useContracts();
  const [name, setName] = useState("");
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState(undefined as string | undefined);
  const [actionError, setActionError] = useState(undefined as string | undefined);

  const save = useCallback(async () => {
    const normalizedName = name.trim();
    if (!normalizedName || saving) return;
    setSaving(true);
    setActionError(undefined);
    setNotice(undefined);
    try {
      const result = await contracts.projects.versions.create({
        projectId,
        requestId: `ui:project-version-create:${crypto.randomUUID()}`,
        name: normalizedName,
      });
      setName("");
      setNotice(`Saved “${result.version.name ?? normalizedName}” at r${result.version.capturedProjectRevision}.`);
      await onSaved(result);
    } catch {
      setActionError("Could not save this project version. Try again.");
    } finally {
      setSaving(false);
    }
  }, [contracts.projects.versions, name, onSaved, projectId, saving]);

  return (
    <>
      <ControlStrip
        hint="AUTO BEFORE AGENT TURNS · SOURCE MEDIA STAYS SHARED"
        label="Save named project version"
        summary="MANUAL CHECKPOINT"
      >
        <TextField
          density="compact"
          disabled={saving}
          label="Version name"
          maxLength={200}
          onChange={setName}
          onKeyDown={(event) => {
            if (event.key === "Enter") void save();
          }}
          placeholder="Name this version · e.g. Approved assembly"
          value={name}
        />
        <Button disabled={!name.trim() || saving} variant="primary" onPress={() => void save()}>
          {saving ? "Saving version…" : "Save version"}
        </Button>
      </ControlStrip>
      {notice ? <Status state="ready">{notice}</Status> : null}
      {actionError ? <Status state="unavailable">{actionError}</Status> : null}
    </>
  );
}
