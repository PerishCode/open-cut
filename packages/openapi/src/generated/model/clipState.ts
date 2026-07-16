import type { TimeRange } from './timeRange';

export interface ClipState {
  assetId: string;
  enabled: boolean;
  id: string;
  linkGroupId?: string;
  revision: string;
  sequenceId: string;
  sourceRange: TimeRange;
  sourceStreamId: string;
  timelineRange: TimeRange;
  tombstoned: boolean;
  trackId: string;
}
