import type { UIChallengeResultRole } from './uIChallengeResultRole';
import type { UIChallengeResultSchema } from './uIChallengeResultSchema';

export interface UIChallengeResult {
  /** @pattern ^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ */
  apiInstanceId: string;
  /** @minimum 0 */
  cellGeneration: number;
  clientInstance: string;
  expiresAt: string;
  /** @minimum 1 */
  installationGeneration: number;
  installationId: string;
  nonce: string;
  origin: string;
  role: UIChallengeResultRole;
  schema: UIChallengeResultSchema;
  /** Base64url canonical challenge bytes */
  signingPayload: string;
}
