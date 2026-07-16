import type { CreatorCaptionGestureSubjectProvenance } from './creatorCaptionGestureSubjectProvenance';
import type { TimeRange } from './timeRange';

export interface CreatorCaptionGestureSubject {
  /** @pattern ^[a-z][a-z0-9_-]{0,63}$ */
  captionAs?: string;
  captionId?: string;
  /** @maxLength 64 */
  language: string;
  provenance: CreatorCaptionGestureSubjectProvenance;
  range: TimeRange;
  /**
     * @minLength 1
     * @maxLength 262144
     */
  text: string;
  trackId: string;
}
