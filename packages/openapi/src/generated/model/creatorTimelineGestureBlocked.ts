import type { CreatorTimelineGestureBlockedKind } from './creatorTimelineGestureBlockedKind';
import type { CreatorTimelineGestureBlockedReason } from './creatorTimelineGestureBlockedReason';
import type { CreatorTimelineGestureBlockedRecoveriesItem } from './creatorTimelineGestureBlockedRecoveriesItem';
import type { CreatorTimelineGestureBlockedScope } from './creatorTimelineGestureBlockedScope';

export interface CreatorTimelineGestureBlocked {
  activityCursor: string;
  baseProjectRevision: string;
  kind: CreatorTimelineGestureBlockedKind;
  reason: CreatorTimelineGestureBlockedReason;
  /** @maxItems 4 */
  recoveries: CreatorTimelineGestureBlockedRecoveriesItem[];
  scope: CreatorTimelineGestureBlockedScope;
  seedClipId: string;
  /** @maxItems 512 */
  subjectAlignmentIds: string[];
  /** @maxItems 64 */
  subjectClipIds: string[];
}
