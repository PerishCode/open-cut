
export type MediaDiagnosticRecovery = typeof MediaDiagnosticRecovery[keyof typeof MediaDiagnosticRecovery];


export const MediaDiagnosticRecovery = {
  'automatic-retry': 'automatic-retry',
  'retry-job': 'retry-job',
  'relink-source': 'relink-source',
  'acquire-resource': 'acquire-resource',
  'adopt-revision': 'adopt-revision',
  'update-runtime': 'update-runtime',
  none: 'none',
} as const;
