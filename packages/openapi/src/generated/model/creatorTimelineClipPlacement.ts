import type { TimeRange } from './timeRange';

export interface CreatorTimelineClipPlacement {
  linked: boolean;
  /** @pattern ^[1-9][0-9]*$ */
  revision: string;
  sourceRange: TimeRange;
  timelineRange: TimeRange;
  trackId: string;
}
