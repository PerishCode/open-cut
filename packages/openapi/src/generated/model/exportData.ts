import type { ExportArtifactData } from './exportArtifactData';
import type { ExportDataPreset } from './exportDataPreset';
import type { ExportDataRecovery } from './exportDataRecovery';
import type { ExportJobData } from './exportJobData';

export interface ExportData {
  activityCursor: string;
  artifact?: ExportArtifactData;
  job: ExportJobData;
  preset: ExportDataPreset;
  projectId: string;
  recovery: ExportDataRecovery;
  replayed: boolean;
  sequenceId: string;
  sequenceRevision: string;
}
