
export type ProductResourceViewState = typeof ProductResourceViewState[keyof typeof ProductResourceViewState];


export const ProductResourceViewState = {
  'not-acquired': 'not-acquired',
  queued: 'queued',
  acquiring: 'acquiring',
  ready: 'ready',
  failed: 'failed',
  cancelled: 'cancelled',
} as const;
