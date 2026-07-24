import { Button } from "@open-cut/components";

export type CreatorWorkspaceActionsProps = {
  importing: boolean;
  ready: boolean;
  syncing: boolean;
  onExit?: () => void;
  onImport(): void;
  onSync(): void;
};

export function CreatorWorkspaceActions({
  importing,
  onExit,
  onImport,
  onSync,
  ready,
  syncing,
}: CreatorWorkspaceActionsProps) {
  return (
    <>
      {onExit ? (
        <Button variant="quiet" onPress={onExit}>
          Projects
        </Button>
      ) : null}
      <Button disabled={!ready || importing || syncing} variant="primary" onPress={onImport}>
        {importing ? "Selecting…" : "Add footage"}
      </Button>
      <Button disabled={importing || syncing} variant="quiet" onPress={onSync}>
        {syncing ? "Syncing…" : "Sync now"}
      </Button>
    </>
  );
}
