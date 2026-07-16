
export type CommandReceiptClass = typeof CommandReceiptClass[keyof typeof CommandReceiptClass];


export const CommandReceiptClass = {
  evidence: 'evidence',
  outcome: 'outcome',
} as const;
