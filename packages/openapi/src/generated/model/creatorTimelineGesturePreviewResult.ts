import type { CreatorTimelineGestureBlocked } from './creatorTimelineGestureBlocked';
import type { CreatorTimelineGesturePreview } from './creatorTimelineGesturePreview';
import type { CreatorTimelineGesturePreviewResultStatus } from './creatorTimelineGesturePreviewResultStatus';

export interface CreatorTimelineGesturePreviewResult {
  blocked?: CreatorTimelineGestureBlocked;
  ready?: CreatorTimelineGesturePreview;
  status: CreatorTimelineGesturePreviewResultStatus;
}
