import type { TimeRange } from './timeRange';

export interface DerivedCaptionOutputInput {
  /** @pattern ^[a-z][a-z0-9_-]{0,63}$ */
  alignmentAs: string;
  /** @pattern ^[a-z][a-z0-9_-]{0,63}$ */
  captionAs: string;
  sourceRange: TimeRange;
  /**
     * @minLength 1
     * @maxLength 262144
     */
  text: string;
  timelineRange: TimeRange;
}
