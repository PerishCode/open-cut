import type { ReactNode } from "react";

export type ControlListProps = Readonly<{
  children: ReactNode;
  /** Accessible name for a flat sequence of operational control rows. */
  label: string;
}>;

/** Groups consecutive ControlStrip rows without adding card or page chrome. */
export function ControlList({ children, label }: ControlListProps) {
  return <section aria-label={label}>{children}</section>;
}
