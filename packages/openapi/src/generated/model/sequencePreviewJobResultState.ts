
export type SequencePreviewJobResultState = typeof SequencePreviewJobResultState[keyof typeof SequencePreviewJobResultState];


export const SequencePreviewJobResultState = {
  blocked: 'blocked',
  queued: 'queued',
  running: 'running',
  succeeded: 'succeeded',
  failed: 'failed',
  cancelled: 'cancelled',
} as const;
