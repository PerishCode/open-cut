import type { TimeRange } from './timeRange';

export interface CaptionAlignmentTarget {
  captionId: string;
  /** @pattern ^[1-9][0-9]*$ */
  captionRevision: string;
  localRange: TimeRange;
}
