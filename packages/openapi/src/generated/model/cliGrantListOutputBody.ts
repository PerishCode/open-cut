import type { CLIGrant } from './cLIGrant';
import type { CLIGrantScopeUpgrade } from './cLIGrantScopeUpgrade';

export interface CliGrantListOutputBody {
  /** @maxItems 256 */
  grants: CLIGrant[];
  /** @maxItems 256 */
  upgrades: CLIGrantScopeUpgrade[];
}
