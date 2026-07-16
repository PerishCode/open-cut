
export type CreatorTimelineAlignmentEffectHandling = typeof CreatorTimelineAlignmentEffectHandling[keyof typeof CreatorTimelineAlignmentEffectHandling];


export const CreatorTimelineAlignmentEffectHandling = {
  'preserve-if-provable': 'preserve-if-provable',
  'mark-stale': 'mark-stale',
  unbind: 'unbind',
} as const;
