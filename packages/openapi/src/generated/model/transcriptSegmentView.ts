import type { TimeRange } from './timeRange';
import type { TranscriptTokenView } from './transcriptTokenView';

export interface TranscriptSegmentView {
  id: string;
  /** @minimum 0 */
  ordinal: number;
  sourceRange: TimeRange;
  /**
     * @minLength 1
     * @maxLength 8192
     */
  text: string;
  /** @maxItems 2048 */
  tokens: TranscriptTokenView[];
}
