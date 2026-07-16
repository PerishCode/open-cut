
export type ExportLineageDataArtifactAvailability = typeof ExportLineageDataArtifactAvailability[keyof typeof ExportLineageDataArtifactAvailability];


export const ExportLineageDataArtifactAvailability = {
  none: 'none',
  ready: 'ready',
  invalid: 'invalid',
  deleted: 'deleted',
} as const;
