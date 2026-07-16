
export type MediaLeaseMimeType = typeof MediaLeaseMimeType[keyof typeof MediaLeaseMimeType];


export const MediaLeaseMimeType = {
  'video/webm': 'video/webm',
  'audio/webm': 'audio/webm',
} as const;
