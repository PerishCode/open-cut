
export type SequencePreviewLeaseResultStatus = typeof SequencePreviewLeaseResultStatus[keyof typeof SequencePreviewLeaseResultStatus];


export const SequencePreviewLeaseResultStatus = {
  empty: 'empty',
  ready: 'ready',
  preparing: 'preparing',
  failed: 'failed',
} as const;
