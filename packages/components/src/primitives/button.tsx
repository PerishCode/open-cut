import type { ReactNode } from "react";

import styles from "./theme.module.css";

export type ButtonVariant = "primary" | "secondary" | "quiet" | "danger";

export type ButtonProps = {
  children: ReactNode;
  disabled?: boolean;
  onPress(): void;
  pressed?: boolean;
  variant?: ButtonVariant;
};

const variantClass: Record<ButtonVariant, string> = {
  danger: styles.buttonDanger ?? "",
  primary: styles.buttonPrimary ?? "",
  quiet: styles.buttonQuiet ?? "",
  secondary: styles.buttonSecondary ?? "",
};

export function Button({ children, disabled = false, onPress, pressed, variant = "secondary" }: ButtonProps) {
  return (
    <button
      aria-pressed={pressed}
      className={`${styles.button} ${variantClass[variant]}`}
      disabled={disabled}
      type="button"
      onClick={onPress}
    >
      {children}
    </button>
  );
}
