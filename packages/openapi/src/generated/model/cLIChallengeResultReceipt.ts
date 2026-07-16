
export type CLIChallengeResultReceipt = typeof CLIChallengeResultReceipt[keyof typeof CLIChallengeResultReceipt];


export const CLIChallengeResultReceipt = {
  none: 'none',
  evidence: 'evidence',
  outcome: 'outcome',
} as const;
