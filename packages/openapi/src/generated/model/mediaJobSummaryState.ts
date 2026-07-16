
export type MediaJobSummaryState = typeof MediaJobSummaryState[keyof typeof MediaJobSummaryState];


export const MediaJobSummaryState = {
  blocked: 'blocked',
  queued: 'queued',
  running: 'running',
  succeeded: 'succeeded',
  failed: 'failed',
  cancelled: 'cancelled',
} as const;
