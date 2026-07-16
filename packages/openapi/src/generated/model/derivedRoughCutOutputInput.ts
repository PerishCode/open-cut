import type { DerivedRoughCutLaneOutputInput } from './derivedRoughCutLaneOutputInput';
import type { TimeRange } from './timeRange';

export interface DerivedRoughCutOutputInput {
  /** @pattern ^[a-z][a-z0-9_-]{0,63}$ */
  alignmentAs: string;
  audio?: DerivedRoughCutLaneOutputInput;
  /** @pattern ^[a-z][a-z0-9_-]{0,63}$ */
  linkGroupAs?: string;
  sourceExcerptId: string;
  sourceRange: TimeRange;
  timelineRange: TimeRange;
  video?: DerivedRoughCutLaneOutputInput;
}
