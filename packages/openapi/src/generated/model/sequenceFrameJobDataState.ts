
export type SequenceFrameJobDataState = typeof SequenceFrameJobDataState[keyof typeof SequenceFrameJobDataState];


export const SequenceFrameJobDataState = {
  blocked: 'blocked',
  queued: 'queued',
  running: 'running',
  succeeded: 'succeeded',
  failed: 'failed',
  cancelled: 'cancelled',
} as const;
