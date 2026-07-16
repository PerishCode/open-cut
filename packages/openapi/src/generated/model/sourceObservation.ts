
export interface SourceObservation {
  /** @pattern ^(0|[1-9][0-9]*)$ */
  byteSize: string;
  /**
     * @minLength 1
     * @maxLength 512
     */
  fileIdentity: string;
  /** @pattern ^(0|-?[1-9][0-9]*)$ */
  modifiedUnixNs: string;
}
