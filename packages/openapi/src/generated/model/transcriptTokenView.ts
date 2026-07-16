import type { TimeRange } from './timeRange';

export interface TranscriptTokenView {
  /**
     * @minimum 0
     * @maximum 10000
     */
  confidenceBasisPoints?: number;
  id: string;
  sourceRange: TimeRange;
  /**
     * @minLength 1
     * @maxLength 512
     */
  text: string;
}
