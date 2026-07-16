
export type CreatorTimelineGestureBlockedReason = typeof CreatorTimelineGestureBlockedReason[keyof typeof CreatorTimelineGestureBlockedReason];


export const CreatorTimelineGestureBlockedReason = {
  'no-change': 'no-change',
  'scope-unavailable': 'scope-unavailable',
  'track-incompatible': 'track-incompatible',
  'range-invalid': 'range-invalid',
  'track-collision': 'track-collision',
  'alignment-preserve-unprovable': 'alignment-preserve-unprovable',
  'closure-limit': 'closure-limit',
} as const;
