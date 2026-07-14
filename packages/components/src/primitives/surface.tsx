import type { ReactNode } from "react";

import styles from "./theme.module.css";

export type SurfaceProps = {
  children: ReactNode;
  label?: string;
};

export function Surface({ children, label }: SurfaceProps) {
  return (
    <main aria-label={label} className={styles.surface}>
      <section className={styles.panel}>{children}</section>
    </main>
  );
}
