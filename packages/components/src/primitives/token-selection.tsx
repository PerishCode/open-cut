import { type KeyboardEvent, useRef, useState } from "react";

import styles from "./token-selection.module.css";

export type TokenSelectionItem<Identifier extends string = string> = Readonly<{
  id: Identifier;
  label: string;
  selected: boolean;
  text: string;
}>;

export type TokenSelectionProps<Identifier extends string = string> = Readonly<{
  disabled?: boolean;
  items: readonly TokenSelectionItem<Identifier>[];
  label: string;
  onSelect(id: Identifier): void;
}>;

export function TokenSelection<Identifier extends string>({
  disabled = false,
  items,
  label,
  onSelect,
}: TokenSelectionProps<Identifier>) {
  const buttonRefs = useRef<(HTMLButtonElement | null)[]>([]);
  const [activeId, setActiveId] = useState<Identifier>();
  const tabbableId =
    (activeId && items.some((item) => item.id === activeId) ? activeId : undefined) ??
    items.find((item) => item.selected)?.id ??
    items[0]?.id;

  const moveFocus = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
    let target: number | undefined;
    if (event.key === "ArrowLeft") target = Math.max(0, index - 1);
    else if (event.key === "ArrowRight") target = Math.min(items.length - 1, index + 1);
    else if (event.key === "Home") target = 0;
    else if (event.key === "End") target = items.length - 1;
    if (target === undefined || target === index) return;
    event.preventDefault();
    buttonRefs.current[target]?.focus();
  };

  return (
    <fieldset aria-label={label} className={styles.tokenSelection} disabled={disabled}>
      {items.map((item, index) => (
        <button
          aria-label={`${item.selected ? "Selected" : "Select"} token ${index + 1} · ${item.label}`}
          aria-pressed={item.selected}
          className={styles.token}
          disabled={disabled}
          key={item.id}
          ref={(node) => {
            buttonRefs.current[index] = node;
          }}
          tabIndex={item.id === tabbableId ? 0 : -1}
          type="button"
          onClick={() => onSelect(item.id)}
          onFocus={() => setActiveId(item.id)}
          onKeyDown={(event) => moveFocus(event, index)}
        >
          {item.text.trim() === "" ? "␠" : item.text}
        </button>
      ))}
    </fieldset>
  );
}
