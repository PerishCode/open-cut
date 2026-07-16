import type { KeyboardEvent } from "react";
import { useEffect, useRef } from "react";

import styles from "./theme.module.css";

export type TextFieldProps = {
  label: string;
  value: string;
  disabled?: boolean;
  focusRequest?: string | number;
  maxLength?: number;
  placeholder?: string;
  onChange(value: string): void;
  onBlur?(): void;
  onFocus?(): void;
  onKeyDown?(event: KeyboardEvent<HTMLInputElement>): void;
};

export function TextField({
  label,
  value,
  disabled = false,
  focusRequest,
  maxLength,
  placeholder,
  onChange,
  onBlur,
  onFocus,
  onKeyDown,
}: TextFieldProps) {
  const fieldRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (focusRequest !== undefined) fieldRef.current?.focus();
  }, [focusRequest]);

  return (
    <label className={styles.field}>
      <span className={styles.fieldLabel}>{label}</span>
      <input
        className={styles.fieldInput}
        disabled={disabled}
        maxLength={maxLength}
        placeholder={placeholder}
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
