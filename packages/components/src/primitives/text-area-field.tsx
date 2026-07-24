import type { KeyboardEvent } from "react";
import { useEffect, useRef } from "react";
import styles from "./theme.module.css";

export type TextAreaFieldProps = {
  label: string;
  value: string;
  density?: "default" | "compact";
  focusRequest?: string | number;
  disabled?: boolean;
  keyboardShortcuts?: string;
  maxLength?: number;
  placeholder?: string;
  rows?: number;
  onChange(value: string): void;
  onBlur?(): void;
  onFocus?(): void;
  onKeyDown?(event: KeyboardEvent<HTMLTextAreaElement>): void;
};

export function TextAreaField({
  label,
  value,
  density = "default",
  focusRequest,
  disabled = false,
  keyboardShortcuts,
  maxLength,
  placeholder,
  rows = 4,
  onChange,
  onBlur,
  onFocus,
  onKeyDown,
}: TextAreaFieldProps) {
  const fieldRef = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    if (focusRequest !== undefined) fieldRef.current?.focus();
  }, [focusRequest]);

  return (
    <label className={`${styles.field} ${density === "compact" ? styles.fieldCompact : ""}`}>
      <span className={styles.fieldLabel}>{label}</span>
      <textarea
        aria-keyshortcuts={keyboardShortcuts}
        className={styles.fieldTextArea}
        disabled={disabled}
        maxLength={maxLength}
        placeholder={placeholder}
        rows={rows}
        ref={fieldRef}
        value={value}
        onBlur={onBlur}
        onChange={(event) => onChange(event.currentTarget.value)}
        onFocus={onFocus}
        onKeyDown={onKeyDown}
      />
    </label>
  );
}
