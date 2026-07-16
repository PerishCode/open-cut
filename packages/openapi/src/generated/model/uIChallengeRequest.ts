
export interface UIChallengeRequest {
  /** @pattern ^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$ */
  clientInstance: string;
  /**
     * @minLength 1
     * @maxLength 512
     */
  origin: string;
}
