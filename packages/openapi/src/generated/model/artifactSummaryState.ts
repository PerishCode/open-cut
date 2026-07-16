
export type ArtifactSummaryState = typeof ArtifactSummaryState[keyof typeof ArtifactSummaryState];


export const ArtifactSummaryState = {
  ready: 'ready',
  evicted: 'evicted',
} as const;
