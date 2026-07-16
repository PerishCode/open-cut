import type { TimeRange } from './timeRange';

export interface TranscriptCorrectionState {
  assetId: string;
  id: string;
  /** @maxLength 64 */
  language: string;
  /**
     * @minLength 1
     * @maxLength 262144
     */
  replacementText: string;
  revision: string;
  /**
     * @minItems 1
     * @maxItems 256
     */
  segmentIds: string[];
  sourceRange: TimeRange;
  tombstoned: boolean;
  transcriptArtifactId: string;
}
