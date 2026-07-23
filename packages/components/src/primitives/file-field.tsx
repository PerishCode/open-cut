import { useState } from "react";

import styles from "./theme.module.css";

export type FileFieldProps = {
  label: string;
  accept?: string;
  disabled?: boolean;
  onSelect(file: File): void;
};

export function FileField({ label, accept, disabled = false, onSelect }: FileFieldProps) {
  const [fileName, setFileName] = useState<string>();
  const select = (file: File) => {
    setFileName(file.name);
    onSelect(file);
  };

  return (
    <label
      className={styles.fileField}
      onDragOver={(event) => {
        if (!disabled) event.preventDefault();
      }}
      onDrop={(event) => {
        event.preventDefault();
        const file = disabled ? undefined : event.dataTransfer.files[0];
        if (file) select(file);
      }}
    >
      <span className={styles.fileFieldLabel}>{label}</span>
      <span className={styles.fileFieldChoice}>
        <span className={styles.fileFieldAction}>Choose file</span>
        <span className={styles.fileFieldName}>{fileName ?? "No file selected"}</span>
      </span>
      <input
        accept={accept}
        aria-label={label}
        className={styles.fileFieldInput}
        disabled={disabled}
        type="file"
        onChange={(event) => {
          const file = event.currentTarget.files?.[0];
          if (file) select(file);
          event.currentTarget.value = "";
        }}
      />
    </label>
  );
}
