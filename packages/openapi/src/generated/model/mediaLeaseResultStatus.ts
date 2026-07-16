
export type MediaLeaseResultStatus = typeof MediaLeaseResultStatus[keyof typeof MediaLeaseResultStatus];


export const MediaLeaseResultStatus = {
  ready: 'ready',
  preparing: 'preparing',
  failed: 'failed',
} as const;
