
export type ExportLineageDataOrigin = typeof ExportLineageDataOrigin[keyof typeof ExportLineageDataOrigin];


export const ExportLineageDataOrigin = {
  agent: 'agent',
  creator: 'creator',
} as const;
