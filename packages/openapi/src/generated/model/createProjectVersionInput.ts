
export interface CreateProjectVersionInput {
  /**
     * @minLength 1
     * @maxLength 200
     */
  name: string;
  /** @pattern ^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$ */
  requestId: string;
}
