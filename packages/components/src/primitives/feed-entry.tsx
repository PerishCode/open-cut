import type { ReactNode, Ref } from "react";

import styles from "./feed-entry.module.css";

export type FeedEntryEmphasis = "default" | "quiet";

export type FeedEntryProps = Readonly<{
  children: ReactNode;
  details?: readonly ReactNode[];
  elementRef?: Ref<HTMLElement>;
  emphasis?: FeedEntryEmphasis;
  hint?: ReactNode;
  label: string;
  summary: ReactNode;
}>;

export function FeedEntry({
  children,
  details = [],
  elementRef,
  emphasis = "default",
  hint,
  label,
  summary,
}: FeedEntryProps) {
  return (
    <article
      aria-label={label}
      className={`${styles.feedEntry} ${emphasis === "quiet" ? styles.feedEntryQuiet : styles.feedEntryDefault}`}
      ref={elementRef}
    >
      <div className={styles.feedEntryMeta}>
        <div className={styles.feedEntrySummary}>{summary}</div>
        {hint ? <div className={styles.feedEntryHint}>{hint}</div> : null}
      </div>
      <div className={styles.feedEntryBody}>{children}</div>
      {details.length > 0 ? (
        <div className={styles.feedEntryDetails}>
          {details.map((detail, index) => (
            <div key={`detail:${index}`}>{detail}</div>
          ))}
        </div>
      ) : null}
    </article>
  );
}
