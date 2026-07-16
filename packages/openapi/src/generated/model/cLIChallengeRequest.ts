import type { CLIChallengeRequestMethod } from './cLIChallengeRequestMethod';
import type { InvocationContext } from './invocationContext';
import type { InvocationPolicyOverride } from './invocationPolicyOverride';

export interface CLIChallengeRequest {
  bodyDigest: string;
  /** @pattern ^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$ */
  clientInstance: string;
  /**
     * @minLength 3
     * @maxLength 128
     */
  command: string;
  commandFingerprint: string;
  context: InvocationContext;
  method: CLIChallengeRequestMethod;
  /**
     * @minLength 1
     * @maxLength 2048
     */
  path: string;
  policyOverride: InvocationPolicyOverride;
  /** @maxLength 4096 */
  query: string;
  /** @pattern ^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$ */
  requestId?: string;
}
