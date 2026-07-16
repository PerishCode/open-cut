
export type CommandReceiptStatus = typeof CommandReceiptStatus[keyof typeof CommandReceiptStatus];


export const CommandReceiptStatus = {
  succeeded: 'succeeded',
  accepted: 'accepted',
  waiting: 'waiting',
  'approval-required': 'approval-required',
  conflict: 'conflict',
  'not-found': 'not-found',
  unavailable: 'unavailable',
  incompatible: 'incompatible',
  invalid: 'invalid',
  failed: 'failed',
} as const;
