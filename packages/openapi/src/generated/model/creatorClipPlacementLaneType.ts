
export type CreatorClipPlacementLaneType = typeof CreatorClipPlacementLaneType[keyof typeof CreatorClipPlacementLaneType];


export const CreatorClipPlacementLaneType = {
  video: 'video',
  audio: 'audio',
} as const;
