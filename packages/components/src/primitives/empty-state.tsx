import type { ReactNode } from "react";

import styles from "./theme.module.css";

export type EmptyStateProps = {
  title: string;
  hint: string;
  action?: ReactNode;
};

export function EmptyState({ title, hint, action }: EmptyStateProps) {
  return (
    <div className={styles.emptyState} role="note">
      <p className={styles.emptyStateTitle}>{title}</p>
      <p className={styles.emptyStateHint}>{hint}</p>
      {action}
    </div>
  );
}
