
export interface RestoreProjectVersionInput {
  /** @pattern ^[1-9][0-9]*$ */
  expectedProjectRevision: string;
  /** @pattern ^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$ */
  requestId: string;
}
