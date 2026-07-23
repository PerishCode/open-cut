import { Button } from "@open-cut/components";

export type CreatorWorkspaceActionsProps = {
  importing: boolean;
  ready: boolean;
  onExit?: () => void;
  onImport(): void;
  onRefresh(): void;
};

export function CreatorWorkspaceActions({
  importing,
  onExit,
  onImport,
  onRefresh,
  ready,
}: CreatorWorkspaceActionsProps) {
  return (
    <>
      {onExit ? (
        <Button variant="quiet" onPress={onExit}>
          Projects
        </Button>
      ) : null}
      <Button disabled={!ready || importing} variant="primary" onPress={onImport}>
        {importing ? "Selecting…" : "Add footage"}
      </Button>
      <Button disabled={importing} variant="quiet" onPress={onRefresh}>
        Refresh reads
      </Button>
    </>
  );
}
