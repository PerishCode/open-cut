
export interface UISessionRequest {
  /**
     * @minLength 43
     * @maxLength 43
     */
  nonce: string;
  /** Base64url Ed25519 signature */
  signature: string;
}
