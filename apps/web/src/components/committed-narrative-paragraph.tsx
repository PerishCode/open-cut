import { Button, ControlStrip } from "@open-cut/components";
import type { AuthoredText } from "@open-cut/contracts";

import { formatLanguageLabel } from "./creator-workspace-presentation.js";

export function CommittedNarrativeParagraph({
  canMoveDown,
  canMoveUp,
  node,
  onEdit,
  onMoveDown,
  onMoveUp,
  onRemove,
  ordinal,
  value,
}: Readonly<{
  canMoveDown: boolean;
  canMoveUp: boolean;
  node: AuthoredText;
  onEdit(): void;
  onMoveDown(): void;
  onMoveUp(): void;
  onRemove(): void;
  ordinal: number;
  value: string;
}>) {
  return (
    <ControlStrip
      hint={`${String(ordinal).padStart(2, "0")} · ${node.purpose.toUpperCase()} · ${formatLanguageLabel(
        node.language,
      )} · r${node.revision} · COMMITTED`}
      label={`Narrative paragraph ${ordinal}`}
      summary={value}
    >
      <Button label={`Edit Narrative paragraph ${ordinal}`} onPress={onEdit}>
        Edit
      </Button>
      <Button disabled={!canMoveUp} label="Move paragraph up" onPress={onMoveUp}>
        Up
      </Button>
      <Button disabled={!canMoveDown} label="Move paragraph down" onPress={onMoveDown}>
        Down
      </Button>
      <Button label="Remove paragraph" onPress={onRemove}>
        Remove
      </Button>
    </ControlStrip>
  );
}

export function NarrativeParagraphStructureControls({
  canMoveDown,
  canMoveUp,
  clean,
  onDone,
  onMoveDown,
  onMoveUp,
  onRemove,
  ordinal,
}: Readonly<{
  canMoveDown: boolean;
  canMoveUp: boolean;
  clean: boolean;
  onDone(): void;
  onMoveDown(): void;
  onMoveUp(): void;
  onRemove(): void;
  ordinal: number;
}>) {
  return (
    <ControlStrip label={`Narrative paragraph ${ordinal} structure actions`}>
      <Button disabled={!clean} label={`Finish editing Narrative paragraph ${ordinal}`} onPress={onDone}>
        Done
      </Button>
      <Button disabled={!canMoveUp || !clean} label="Move paragraph up" onPress={onMoveUp}>
        Up
      </Button>
      <Button disabled={!canMoveDown || !clean} label="Move paragraph down" onPress={onMoveDown}>
        Down
      </Button>
      <Button disabled={!clean} label="Remove paragraph" onPress={onRemove}>
        Remove
      </Button>
    </ControlStrip>
  );
}
