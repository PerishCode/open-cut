import type { ExportStartInputPreset } from './exportStartInputPreset';

export interface ExportStartInput {
  preset: ExportStartInputPreset;
  /** @pattern ^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$ */
  requestId: string;
  sequenceRevision: string;
}
