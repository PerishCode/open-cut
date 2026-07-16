
export type MediaDiagnosticCode = typeof MediaDiagnosticCode[keyof typeof MediaDiagnosticCode];


export const MediaDiagnosticCode = {
  'source-proxy-integrity-rejected': 'source-proxy-integrity-rejected',
  'source-proxy-job-failed': 'source-proxy-job-failed',
  'source-proxy-job-cancelled': 'source-proxy-job-cancelled',
  'sequence-preview-integrity-rejected': 'sequence-preview-integrity-rejected',
  'sequence-preview-job-failed': 'sequence-preview-job-failed',
  'sequence-preview-job-cancelled': 'sequence-preview-job-cancelled',
} as const;
