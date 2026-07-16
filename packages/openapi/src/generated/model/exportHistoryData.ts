import type { ExportLineageData } from './exportLineageData';

export interface ExportHistoryData {
  activityCursor: string;
  /** @maxItems 50 */
  lineages: ExportLineageData[];
  /** @maxLength 512 */
  nextAfter?: string;
}
