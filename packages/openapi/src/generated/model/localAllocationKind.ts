
export type LocalAllocationKind = typeof LocalAllocationKind[keyof typeof LocalAllocationKind];


export const LocalAllocationKind = {
  'narrative-node': 'narrative-node',
  caption: 'caption',
  alignment: 'alignment',
  clip: 'clip',
  'link-group': 'link-group',
  asset: 'asset',
  'transcript-correction': 'transcript-correction',
} as const;
