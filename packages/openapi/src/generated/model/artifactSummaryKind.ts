
export type ArtifactSummaryKind = typeof ArtifactSummaryKind[keyof typeof ArtifactSummaryKind];


export const ArtifactSummaryKind = {
  'media-facts': 'media-facts',
  'frame-sample-set': 'frame-sample-set',
  proxy: 'proxy',
  'render-input': 'render-input',
  waveform: 'waveform',
  transcript: 'transcript',
} as const;
