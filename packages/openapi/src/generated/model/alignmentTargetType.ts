
export type AlignmentTargetType = typeof AlignmentTargetType[keyof typeof AlignmentTargetType];


export const AlignmentTargetType = {
  caption: 'caption',
  clip: 'clip',
  timeline: 'timeline',
} as const;
