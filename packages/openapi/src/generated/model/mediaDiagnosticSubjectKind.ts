
export type MediaDiagnosticSubjectKind = typeof MediaDiagnosticSubjectKind[keyof typeof MediaDiagnosticSubjectKind];


export const MediaDiagnosticSubjectKind = {
  asset: 'asset',
  'media-job': 'media-job',
  'work-job': 'work-job',
  artifact: 'artifact',
} as const;
