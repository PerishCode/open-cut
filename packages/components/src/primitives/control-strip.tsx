import type { KeyboardEventHandler, ReactNode, Ref } from "react";

import editorial from "./control-strip.module.css";
import styles from "./theme.module.css";

export type ControlStripPresentation = "compact" | "editorial";

export type ControlStripProps = Readonly<{
  /** Accessible name for the strip as a group. */
  label: string;
  /**
   * Compact: identity/range label (ellipsized).
   * Editorial: primary creative body (wraps).
   */
  summary?: ReactNode;
  /**
   * Optional exact trail beside a compact summary. It ellipsizes before the
   * human summary text does; ignored in editorial presentation.
   */
  summaryDetail?: ReactNode;
  /**
   * Compact: readiness/policy hint beside the summary.
   * Editorial: exact ordinal/kind/range/revision metadata below the body.
   */
  hint?: ReactNode;
  /**
   * `compact` keeps operational log density for Timeline/Versions/Agent.
   * `editorial` elevates body text for Story/Narrative content rows.
   */
  presentation?: ControlStripPresentation;
  /** Optional local shortcuts when the strip itself owns keyboard focus. */
  keyboardShortcuts?: string;
  onKeyDown?: KeyboardEventHandler<HTMLElement>;
  /** Optional horizontal choice and action controls. */
  children?: ReactNode;
  /**
   * Optional trailing primary verb (or status) for this row. Keeps full
   * button chrome in a stable trailing column instead of flattening into
   * the body row.
   */
  action?: ReactNode;
  /** Optional host ref, e.g. for reveal-into-view behavior. */
  elementRef?: Ref<HTMLElement>;
}>;

/**
 * Dense control chrome: compact accessory rows by default, with an opt-in
 * editorial presentation for content-first Story/Narrative strips.
 */
export function ControlStrip({
  label,
  summary,
  summaryDetail,
  hint,
  presentation = "compact",
  keyboardShortcuts,
  onKeyDown,
  children,
  action,
  elementRef,
}: ControlStripProps) {
  const editorialMode = presentation === "editorial";
  const classNames = [styles.controlStrip];
  if (editorialMode) classNames.push(editorial.editorial);
  if (action) classNames.push(editorial.withAction);
  const lead = (
    <>
      {summary || hint ? (
        <div className={editorialMode ? editorial.editorialMeta : styles.controlStripMeta}>
          {summary ? (
            <div
              className={
                editorialMode
                  ? editorial.editorialSummary
                  : summaryDetail !== undefined
                    ? `${styles.controlStripSummary} ${editorial.summarySplit}`
                    : styles.controlStripSummary
              }
            >
              {!editorialMode && summaryDetail !== undefined ? (
                <>
                  <span className={editorial.summaryName}>{summary}</span>
                  <span className={editorial.summaryDetail}>{summaryDetail}</span>
                </>
              ) : (
                summary
              )}
            </div>
          ) : null}
          {hint ? (
            <div className={editorialMode ? editorial.editorialHint : styles.controlStripHint}>{hint}</div>
          ) : null}
        </div>
      ) : null}
      {children ? (
        <div
          className={editorialMode ? `${styles.controlStripBody} ${editorial.editorialBody}` : styles.controlStripBody}
        >
          {children}
        </div>
      ) : null}
    </>
  );
  return (
    <section
      aria-keyshortcuts={keyboardShortcuts}
      aria-label={label}
      className={classNames.join(" ")}
      ref={elementRef}
      tabIndex={onKeyDown ? 0 : undefined}
      onKeyDown={onKeyDown}
    >
      {action ? (
        <>
          <div className={editorial.lead}>{lead}</div>
          <div className={editorial.action}>{action}</div>
        </>
      ) : (
        lead
      )}
    </section>
  );
}
