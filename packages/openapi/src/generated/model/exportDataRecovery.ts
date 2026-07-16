
export type ExportDataRecovery = typeof ExportDataRecovery[keyof typeof ExportDataRecovery];


export const ExportDataRecovery = {
  'retry-job': 'retry-job',
  'relink-source': 'relink-source',
  'acquire-resource': 'acquire-resource',
  'adopt-revision': 'adopt-revision',
  'update-runtime': 'update-runtime',
  none: 'none',
} as const;
