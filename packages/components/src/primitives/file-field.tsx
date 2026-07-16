import styles from "./theme.module.css";

export type FileFieldProps = {
  label: string;
  accept?: string;
  disabled?: boolean;
  onSelect(file: File): void;
};

export function FileField({ label, accept, disabled = false, onSelect }: FileFieldProps) {
  return (
    <label
      className={styles.fileField}
      onDragOver={(event) => {
        if (!disabled) event.preventDefault();
      }}
      onDrop={(event) => {
        event.preventDefault();
        const file = disabled ? undefined : event.dataTransfer.files[0];
        if (file) onSelect(file);
      }}
    >
      <span className={styles.fileFieldLabel}>{label}</span>
      <input
        accept={accept}
        className={styles.fileFieldInput}
        disabled={disabled}
        type="file"
        onChange={(event) => {
          const file = event.currentTarget.files?.[0];
          if (file) onSelect(file);
          event.currentTarget.value = "";
        }}
      />
    </label>
  );
}
