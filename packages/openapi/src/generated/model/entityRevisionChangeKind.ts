
export type EntityRevisionChangeKind = typeof EntityRevisionChangeKind[keyof typeof EntityRevisionChangeKind];


export const EntityRevisionChangeKind = {
  'narrative-document': 'narrative-document',
  'narrative-node': 'narrative-node',
  sequence: 'sequence',
  track: 'track',
  caption: 'caption',
  alignment: 'alignment',
  clip: 'clip',
  'link-group': 'link-group',
  asset: 'asset',
  'transcript-correction': 'transcript-correction',
} as const;
