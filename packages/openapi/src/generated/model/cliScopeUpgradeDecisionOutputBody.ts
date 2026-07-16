import type { CLIGrant } from './cLIGrant';
import type { CLIGrantScopeUpgrade } from './cLIGrantScopeUpgrade';

export interface CliScopeUpgradeDecisionOutputBody {
  grant: CLIGrant;
  upgrade: CLIGrantScopeUpgrade;
}
