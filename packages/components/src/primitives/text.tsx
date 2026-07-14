import type { ReactNode } from "react";

import styles from "./theme.module.css";

export type TextProps = {
  children: ReactNode;
  tone?: "eyebrow" | "body";
};

export function Text({ children, tone = "body" }: TextProps) {
  return <p className={tone === "eyebrow" ? styles.eyebrow : styles.body}>{children}</p>;
}
