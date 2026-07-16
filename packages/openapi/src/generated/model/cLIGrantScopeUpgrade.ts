import type { CLIGrantScopeUpgradeStatus } from './cLIGrantScopeUpgradeStatus';

export interface CLIGrantScopeUpgrade {
  createdAt: string;
  decidedAt?: string;
  expiresAt: string;
  /** @minimum 1 */
  fromRevision: string;
  grantId: string;
  id: string;
  requestedScopeDigest: string;
  /** @maxItems 64 */
  requestedScopes: string[];
  status: CLIGrantScopeUpgradeStatus;
}
