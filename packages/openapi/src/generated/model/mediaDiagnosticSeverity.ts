
export type MediaDiagnosticSeverity = typeof MediaDiagnosticSeverity[keyof typeof MediaDiagnosticSeverity];


export const MediaDiagnosticSeverity = {
  degraded: 'degraded',
  blocking: 'blocking',
} as const;
