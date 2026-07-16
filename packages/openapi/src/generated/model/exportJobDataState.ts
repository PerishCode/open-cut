
export type ExportJobDataState = typeof ExportJobDataState[keyof typeof ExportJobDataState];


export const ExportJobDataState = {
  blocked: 'blocked',
  queued: 'queued',
  running: 'running',
  succeeded: 'succeeded',
  failed: 'failed',
  cancelled: 'cancelled',
} as const;
