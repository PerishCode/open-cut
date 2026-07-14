import type { ReactNode } from "react";

import styles from "./theme.module.css";

export type ButtonProps = {
  children: ReactNode;
  disabled?: boolean;
  onPress(): void;
};

export function Button({ children, disabled = false, onPress }: ButtonProps) {
  return (
    <button className={styles.button} disabled={disabled} type="button" onClick={onPress}>
      {children}
    </button>
  );
}
