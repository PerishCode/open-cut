import type { TimeRange } from './timeRange';

export interface TranscriptCorrectionView {
  /**
     * @minLength 1
     * @maxLength 262144
     */
  effectiveText: string;
  id: string;
  /** @maxLength 64 */
  language: string;
  /**
     * @minLength 1
     * @maxLength 262144
     */
  originalText: string;
  revision: string;
  /**
     * @minItems 1
     * @maxItems 256
     */
  segmentIds: string[];
  sourceRange: TimeRange;
}
