
export type SourceGrantSummaryKind = typeof SourceGrantSummaryKind[keyof typeof SourceGrantSummaryKind];


export const SourceGrantSummaryKind = {
  'local-path-v1': 'local-path-v1',
  'mac-security-scoped-bookmark-v1': 'mac-security-scoped-bookmark-v1',
} as const;
