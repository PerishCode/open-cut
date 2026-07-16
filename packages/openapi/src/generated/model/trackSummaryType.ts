
export type TrackSummaryType = typeof TrackSummaryType[keyof typeof TrackSummaryType];


export const TrackSummaryType = {
  video: 'video',
  audio: 'audio',
  caption: 'caption',
} as const;
