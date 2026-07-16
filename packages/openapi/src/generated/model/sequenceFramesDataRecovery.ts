
export type SequenceFramesDataRecovery = typeof SequenceFramesDataRecovery[keyof typeof SequenceFramesDataRecovery];


export const SequenceFramesDataRecovery = {
  'retry-job': 'retry-job',
  'relink-source': 'relink-source',
  'acquire-resource': 'acquire-resource',
  'adopt-revision': 'adopt-revision',
  'update-runtime': 'update-runtime',
  none: 'none',
} as const;
