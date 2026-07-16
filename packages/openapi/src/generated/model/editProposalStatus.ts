
export type EditProposalStatus = typeof EditProposalStatus[keyof typeof EditProposalStatus];


export const EditProposalStatus = {
  open: 'open',
  applied: 'applied',
  stale: 'stale',
  cancelled: 'cancelled',
} as const;
