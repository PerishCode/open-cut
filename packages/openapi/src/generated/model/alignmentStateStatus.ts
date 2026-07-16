
export type AlignmentStateStatus = typeof AlignmentStateStatus[keyof typeof AlignmentStateStatus];


export const AlignmentStateStatus = {
  exact: 'exact',
  stale: 'stale',
  unbound: 'unbound',
} as const;
