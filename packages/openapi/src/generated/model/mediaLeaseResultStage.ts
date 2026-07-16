
export type MediaLeaseResultStage = typeof MediaLeaseResultStage[keyof typeof MediaLeaseResultStage];


export const MediaLeaseResultStage = {
  proxy: 'proxy',
  integrity: 'integrity',
  render: 'render',
} as const;
