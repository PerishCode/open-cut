import type { CLIGrantStatus } from './cLIGrantStatus';

export interface CLIGrant {
  agentId: string;
  createdAt: string;
  decidedAt?: string;
  expiresAt: string;
  id: string;
  installationId: string;
  publicKeyFingerprint: string;
  /** @minimum 1 */
  revision: string;
  revokedAt?: string;
  scopeDigest: string;
  /** @maxItems 64 */
  scopes: string[];
  status: CLIGrantStatus;
}
