
export type MediaJobSummaryKind = typeof MediaJobSummaryKind[keyof typeof MediaJobSummaryKind];


export const MediaJobSummaryKind = {
  identify: 'identify',
  probe: 'probe',
  'frame-sample-set': 'frame-sample-set',
  proxy: 'proxy',
  'render-input': 'render-input',
  waveform: 'waveform',
  transcript: 'transcript',
} as const;
