import type { CreatorClipPlacementLaneInput } from './creatorClipPlacementLaneInput';
import type { RationalTime } from './rationalTime';
import type { TimeRange } from './timeRange';

export interface CreatorClipPlacementPreviewInput {
  acceptedFingerprint: string;
  assetId: string;
  /** @pattern ^[1-9][0-9]*$ */
  assetRevision: string;
  audio?: CreatorClipPlacementLaneInput;
  /** @pattern ^[a-z][a-z0-9_-]{0,47}$ */
  localPrefix: string;
  sourceRange: TimeRange;
  timelineStart: RationalTime;
  video?: CreatorClipPlacementLaneInput;
}
