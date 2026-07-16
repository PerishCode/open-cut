
export interface RationalTime {
  /** @minimum 1 */
  scale: number;
  /** @pattern ^(0|-[1-9][0-9]*|[1-9][0-9]*)$ */
  value: string;
}
