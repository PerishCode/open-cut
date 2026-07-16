
export type AlignmentTargetInputType = typeof AlignmentTargetInputType[keyof typeof AlignmentTargetInputType];


export const AlignmentTargetInputType = {
  caption: 'caption',
  clip: 'clip',
  timeline: 'timeline',
} as const;
