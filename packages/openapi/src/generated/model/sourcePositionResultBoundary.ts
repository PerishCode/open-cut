
export type SourcePositionResultBoundary = typeof SourcePositionResultBoundary[keyof typeof SourcePositionResultBoundary];


export const SourcePositionResultBoundary = {
  'video-presentation': 'video-presentation',
  'audio-sample': 'audio-sample',
  'coverage-end': 'coverage-end',
} as const;
