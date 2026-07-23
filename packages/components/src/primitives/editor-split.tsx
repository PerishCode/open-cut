import type { ReactNode } from "react";

import styles from "./theme.module.css";

export type EditorSplitProps = Readonly<{
  primary: ReactNode;
  primaryLabel: string;
  secondary: ReactNode;
  secondaryLabel: string;
}>;

export function EditorSplit({ primary, primaryLabel, secondary, secondaryLabel }: EditorSplitProps) {
  return (
    <div className={styles.editorSplit}>
      <section aria-label={primaryLabel} className={styles.editorSplitPrimary}>
        {primary}
      </section>
      <aside aria-label={secondaryLabel} className={styles.editorSplitSecondary}>
        {secondary}
      </aside>
    </div>
  );
}
