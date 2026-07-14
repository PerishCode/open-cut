import type { ReactNode } from "react";

import styles from "./theme.module.css";

export type StackProps = {
  children: ReactNode;
  spacing?: "compact" | "regular";
};

export function Stack({ children, spacing = "regular" }: StackProps) {
  return <div className={spacing === "compact" ? styles.stackCompact : styles.stack}>{children}</div>;
}
