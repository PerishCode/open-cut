
export type NormalizedEditOperationType = typeof NormalizedEditOperationType[keyof typeof NormalizedEditOperationType];


export const NormalizedEditOperationType = {
  'put-narrative-node': 'put-narrative-node',
  'put-transcript-correction': 'put-transcript-correction',
  'put-caption': 'put-caption',
  'put-alignment': 'put-alignment',
  'put-asset': 'put-asset',
  'put-clip': 'put-clip',
  'put-link-group': 'put-link-group',
} as const;
