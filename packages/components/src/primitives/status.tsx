import type { ReactNode } from "react";

import styles from "./theme.module.css";

export type StatusProps = {
  children: ReactNode;
  state: "ready" | "pending" | "unavailable";
};

export function Status({ children, state }: StatusProps) {
  return (
    <div className={styles.status} role="status">
      <span aria-hidden="true" className={styles[state]} />
      <span>{children}</span>
    </div>
  );
}
