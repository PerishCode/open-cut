import type { CreatorCaptionGesturePreviewInputAlignmentHandling } from './creatorCaptionGesturePreviewInputAlignmentHandling';
import type { CreatorCaptionGesturePreviewInputKind } from './creatorCaptionGesturePreviewInputKind';
import type { TimeRange } from './timeRange';

export interface CreatorCaptionGesturePreviewInput {
  alignmentHandling?: CreatorCaptionGesturePreviewInputAlignmentHandling;
  /** @pattern ^[a-z][a-z0-9_-]{0,63}$ */
  captionAs?: string;
  captionId?: string;
  /** @pattern ^[1-9][0-9]*$ */
  captionRevision?: string;
  kind: CreatorCaptionGesturePreviewInputKind;
  /** @maxLength 64 */
  language?: string;
  range?: TimeRange;
  /** @maxLength 262144 */
  text?: string;
  trackId: string;
  /** @pattern ^[1-9][0-9]*$ */
  trackRevision: string;
}
