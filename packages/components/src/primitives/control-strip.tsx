import type { KeyboardEventHandler, ReactNode } from "react";

import styles from "./theme.module.css";

export type ControlStripProps = Readonly<{
  /** Accessible name for the strip as a group. */
  label: string;
  /** Compact identity / range line rendered above the control row. */
  summary?: ReactNode;
  /** Optional readiness or policy hint, ellipsized beside the summary. */
  hint?: ReactNode;
  /** Optional local shortcuts when the strip itself owns keyboard focus. */
  keyboardShortcuts?: string;
  onKeyDown?: KeyboardEventHandler<HTMLElement>;
  /** Horizontal choice and action controls. */
  children: ReactNode;
}>;

/**
 * Compact control chrome for editor canvases: keeps policy choices in a dense
 * accessory row of the Timeline editor unit without product semantics or raw
 * styling props.
 */
export function ControlStrip({ label, summary, hint, keyboardShortcuts, onKeyDown, children }: ControlStripProps) {
  return (
    <section
      aria-keyshortcuts={keyboardShortcuts}
      aria-label={label}
      className={styles.controlStrip}
      tabIndex={onKeyDown ? 0 : undefined}
      onKeyDown={onKeyDown}
    >
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
