
export type AgentContextAttachmentKind = typeof AgentContextAttachmentKind[keyof typeof AgentContextAttachmentKind];


export const AgentContextAttachmentKind = {
  asset: 'asset',
  'transcript-segment': 'transcript-segment',
  'narrative-node': 'narrative-node',
  clip: 'clip',
  caption: 'caption',
  track: 'track',
  'sequence-point': 'sequence-point',
  'sequence-range': 'sequence-range',
} as const;
