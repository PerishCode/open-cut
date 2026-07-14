import type { ReactNode } from "react";

import styles from "./theme.module.css";

export type HeadingProps = {
  children: ReactNode;
};

export function Heading({ children }: HeadingProps) {
  return <h1 className={styles.heading}>{children}</h1>;
}
