
export interface EditApplyInput {
  /** @pattern ^sha256:[0-9a-f]{64}$ */
  proposalDigest: string;
  /** @pattern ^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$ */
  requestId: string;
}
