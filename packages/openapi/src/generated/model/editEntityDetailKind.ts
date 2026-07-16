
export type EditEntityDetailKind = typeof EditEntityDetailKind[keyof typeof EditEntityDetailKind];


export const EditEntityDetailKind = {
  'narrative-node': 'narrative-node',
  'transcript-correction': 'transcript-correction',
  caption: 'caption',
  alignment: 'alignment',
  clip: 'clip',
  'link-group': 'link-group',
} as const;
