import type { CLIChallengeResultReceipt } from './cLIChallengeResultReceipt';
import type { CLIChallengeResultRole } from './cLIChallengeResultRole';
import type { CLIChallengeResultSchema } from './cLIChallengeResultSchema';
import type { InvocationContext } from './invocationContext';
import type { InvocationPolicySnapshot } from './invocationPolicySnapshot';

export interface CLIChallengeResult {
  apiInstanceId: string;
  bodyDigest: string;
  /** @minimum 0 */
  cellGeneration: number;
  clientInstance: string;
  command: string;
  commandFingerprint: string;
  context: InvocationContext;
  expiresAt: string;
  grantId: string;
  grantRevision?: string;
  grantScopeDigest?: string;
  inputDigest: string;
  /** @minimum 1 */
  installationGeneration: number;
  installationId: string;
  invocationId: string;
  method: string;
  nonce: string;
  path: string;
  policy: InvocationPolicySnapshot;
  query: string;
  receipt: CLIChallengeResultReceipt;
  requestId?: string;
  requiredScope: string;
  role: CLIChallengeResultRole;
  schema: CLIChallengeResultSchema;
  /** Base64url canonical challenge bytes */
  signingPayload: string;
}
