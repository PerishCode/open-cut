
export type SourceGrantSummaryState = typeof SourceGrantSummaryState[keyof typeof SourceGrantSummaryState];


export const SourceGrantSummaryState = {
  active: 'active',
  revoked: 'revoked',
  unavailable: 'unavailable',
} as const;
