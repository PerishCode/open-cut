
export interface PlatformSourceSelection {
  /** @maxLength 65536 */
  bookmark?: string;
  /**
     * @minLength 1
     * @maxLength 32768
     */
  path: string;
  /** @pattern ^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$ */
  requestId: string;
}
