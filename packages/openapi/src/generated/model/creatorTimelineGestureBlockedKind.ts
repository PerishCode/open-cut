
export type CreatorTimelineGestureBlockedKind = typeof CreatorTimelineGestureBlockedKind[keyof typeof CreatorTimelineGestureBlockedKind];


export const CreatorTimelineGestureBlockedKind = {
  move: 'move',
  trim: 'trim',
  split: 'split',
  remove: 'remove',
} as const;
