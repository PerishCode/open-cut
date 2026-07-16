
export type EntityPreconditionKind = typeof EntityPreconditionKind[keyof typeof EntityPreconditionKind];


export const EntityPreconditionKind = {
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
