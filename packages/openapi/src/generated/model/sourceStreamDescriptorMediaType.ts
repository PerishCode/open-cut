
export type SourceStreamDescriptorMediaType = typeof SourceStreamDescriptorMediaType[keyof typeof SourceStreamDescriptorMediaType];


export const SourceStreamDescriptorMediaType = {
  video: 'video',
  audio: 'audio',
  subtitle: 'subtitle',
  data: 'data',
  attachment: 'attachment',
  other: 'other',
} as const;
