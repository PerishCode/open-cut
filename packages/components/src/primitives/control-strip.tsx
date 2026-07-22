import type { ReactNode } from "react";

import styles from "./theme.module.css";

export type ControlStripProps = Readonly<{
  /** Accessible name for the strip as a group. */
  label: string;
  /** Compact identity / range line rendered above the control row. */
  summary?: ReactNode;
  /** Optional readiness or policy hint, ellipsized beside the summary. */
  hint?: ReactNode;
  /** Horizontal choice and action controls. */
  children: ReactNode;
}>;

/**
 * Compact control chrome for editor canvases: keeps policy choices in a dense
 * accessory row of the Timeline editor unit without product semantics or raw
 * styling props.
 */
export function ControlStrip({ label, summary, hint, children }: ControlStripProps) {
  return (
    <section aria-label={label} className={styles.controlStrip}>
      {summary || hint ? (
        <div className={styles.controlStripMeta}>
          {summary ? <div className={styles.controlStripSummary}>{summary}</div> : null}
          {hint ? <div className={styles.controlStripHint}>{hint}</div> : null}
        </div>
      ) : null}
      <div className={styles.controlStripBody}>{children}</div>
    </section>
  );
}
