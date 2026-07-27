import type { ReactNode } from "react";

import styles from "./theme.module.css";

export type HeadingProps = {
  children: ReactNode;
  level?: 1 | 2 | 3;
  tone?: "title" | "eyebrow";
};

export function Heading({ children, level = 1, tone = "title" }: HeadingProps) {
  const Tag = `h${level}` as const;
  return <Tag className={tone === "eyebrow" ? styles.eyebrow : styles.heading}>{children}</Tag>;
}
