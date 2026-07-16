import type { CreatorTimelineGesturePreviewInputAlignmentHandling } from './creatorTimelineGesturePreviewInputAlignmentHandling';
import type { CreatorTimelineGesturePreviewInputKind } from './creatorTimelineGesturePreviewInputKind';
import type { CreatorTimelineGesturePreviewInputScope } from './creatorTimelineGesturePreviewInputScope';
import type { RationalTime } from './rationalTime';
import type { TimeRange } from './timeRange';

export interface CreatorTimelineGesturePreviewInput {
  alignmentHandling: CreatorTimelineGesturePreviewInputAlignmentHandling;
  clipId: string;
  /** @pattern ^[1-9][0-9]*$ */
  clipRevision: string;
  kind: CreatorTimelineGesturePreviewInputKind;
  /** @pattern ^[a-z][a-z0-9_-]{0,39}$ */
  localPrefix?: string;
  scope: CreatorTimelineGesturePreviewInputScope;
  sourceRange?: TimeRange;
  splitAt?: RationalTime;
  timelineRange?: TimeRange;
  timelineStart?: RationalTime;
  trackId?: string;
  /** @pattern ^[1-9][0-9]*$ */
  trackRevision?: string;
}
