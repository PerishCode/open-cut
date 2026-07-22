import type { ReactNode } from "react";

import styles from "./theme.module.css";

export type PanelDockProps = {
  children: ReactNode;
  footer?: ReactNode;
  header?: ReactNode;
  label: string;
};

export function PanelDock({ children, footer, header, label }: PanelDockProps) {
  return (
    <section aria-label={label} className={styles.panelDock}>
      {header ? <div className={styles.panelDockHeader}>{header}</div> : null}
      <div className={styles.panelDockBody}>{children}</div>
      {footer ? <div className={styles.panelDockFooter}>{footer}</div> : null}
    </section>
  );
}
