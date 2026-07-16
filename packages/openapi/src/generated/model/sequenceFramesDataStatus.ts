
export type SequenceFramesDataStatus = typeof SequenceFramesDataStatus[keyof typeof SequenceFramesDataStatus];


export const SequenceFramesDataStatus = {
  accepted: 'accepted',
  ready: 'ready',
  failed: 'failed',
} as const;
