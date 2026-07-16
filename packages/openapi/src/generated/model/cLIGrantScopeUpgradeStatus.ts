
export type CLIGrantScopeUpgradeStatus = typeof CLIGrantScopeUpgradeStatus[keyof typeof CLIGrantScopeUpgradeStatus];


export const CLIGrantScopeUpgradeStatus = {
  pending: 'pending',
  approved: 'approved',
  denied: 'denied',
  expired: 'expired',
  superseded: 'superseded',
} as const;
