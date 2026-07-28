import type { ReactNode, Ref } from "react";

import styles from "./theme.module.css";

export type StackProps = {
  children: ReactNode;
  spacing?: "compact" | "regular";
  /** Optional element ref for scroll anchoring by the owning view. */
  elementRef?: Ref<HTMLDivElement>;
};

export function Stack({ children, spacing = "regular", elementRef }: StackProps) {
  return (
    <div className={spacing === "compact" ? styles.stackCompact : styles.stack} ref={elementRef}>
      {children}
    </div>
  );
}
