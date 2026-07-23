import type { ReactNode, Ref } from "react";

import styles from "./theme.module.css";

export type ResourceCardProps = {
  actions?: ReactNode;
  children?: ReactNode;
  details?: readonly ReactNode[];
  elementRef?: Ref<HTMLElement>;
  eyebrow?: ReactNode;
  selected?: boolean;
  status?: ReactNode;
  title: ReactNode;
};

export function ResourceCard({
  actions,
  children,
  details = [],
  elementRef,
  eyebrow,
  selected = false,
  status,
  title,
}: ResourceCardProps) {
  return (
    <article aria-current={selected ? "true" : undefined} className={styles.resourceCard} ref={elementRef}>
      <div className={styles.resourceCardHeader}>
        <div className={styles.resourceCardTitleGroup}>
          {eyebrow ? <div className={styles.resourceCardEyebrow}>{eyebrow}</div> : null}
          <div className={styles.resourceCardTitle}>{title}</div>
        </div>
        {status ? <div className={styles.resourceCardStatus}>{status}</div> : null}
      </div>
      {children || details.length > 0 ? (
        <div className={styles.resourceCardBody}>
          {children}
          {details.map((detail, index) => (
            <div className={styles.resourceCardDetail} key={`detail:${index}`}>
              {detail}
            </div>
          ))}
        </div>
      ) : null}
      {actions ? <div className={styles.resourceCardActions}>{actions}</div> : null}
    </article>
  );
}
