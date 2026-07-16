import type { TimeRange } from './timeRange';

export interface ClipAlignmentTarget {
  clipId: string;
  /** @pattern ^[1-9][0-9]*$ */
  clipRevision: string;
  localRange: TimeRange;
}
