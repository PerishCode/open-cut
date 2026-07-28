import type { ClipPlacement } from './clipPlacement';
import type { TimeRange } from './timeRange';

export interface ClipState {
  assetId: string;
  enabled: boolean;
  id: string;
  linkGroupId?: string;
  placement?: ClipPlacement;
  revision: string;
  sequenceId: string;
  sourceRange: TimeRange;
  sourceStreamId: string;
  timelineRange: TimeRange;
  tombstoned: boolean;
  trackId: string;
}
