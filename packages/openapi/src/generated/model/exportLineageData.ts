import type { ExportData } from './exportData';
import type { ExportLineageDataArtifactAvailability } from './exportLineageDataArtifactAvailability';
import type { ExportLineageDataOrigin } from './exportLineageDataOrigin';

export interface ExportLineageData {
  artifactAvailability: ExportLineageDataArtifactAvailability;
  attemptCount: string;
  export: ExportData;
  origin: ExportLineageDataOrigin;
  rootCreatedAt: string;
}
