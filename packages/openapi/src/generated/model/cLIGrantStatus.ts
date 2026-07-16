
export type CLIGrantStatus = typeof CLIGrantStatus[keyof typeof CLIGrantStatus];


export const CLIGrantStatus = {
  pending: 'pending',
  active: 'active',
  denied: 'denied',
  revoked: 'revoked',
  expired: 'expired',
} as const;
