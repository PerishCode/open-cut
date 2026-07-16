
export type SequencePreviewLeaseRequestOperation = typeof SequencePreviewLeaseRequestOperation[keyof typeof SequencePreviewLeaseRequestOperation];


export const SequencePreviewLeaseRequestOperation = {
  prepare: 'prepare',
  continue: 'continue',
  retry: 'retry',
} as const;
