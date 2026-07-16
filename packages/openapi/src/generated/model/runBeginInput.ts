
export interface RunBeginInput {
  /**
     * @minLength 1
     * @maxLength 4000
     */
  intent: string;
  /** @pattern ^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$ */
  requestId: string;
}
