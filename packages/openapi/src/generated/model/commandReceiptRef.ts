
export interface CommandReceiptRef {
  /**
     * @minLength 1
     * @maxLength 128
     */
  id: string;
  /**
     * @minLength 1
     * @maxLength 64
     */
  kind: string;
  /** @pattern ^[1-9][0-9]*$ */
  revision?: string;
}
