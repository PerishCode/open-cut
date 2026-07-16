import type { TimeRange } from './timeRange';

export interface TimelineAlignmentTarget {
  range: TimeRange;
  /** @pattern ^[1-9][0-9]*$ */
  sequenceRevision: string;
}
