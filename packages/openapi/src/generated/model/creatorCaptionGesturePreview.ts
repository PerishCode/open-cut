import type { CreatorCaptionAlignmentEffect } from './creatorCaptionAlignmentEffect';
import type { CreatorCaptionGesturePreviewKind } from './creatorCaptionGesturePreviewKind';
import type { CreatorCaptionGestureSubject } from './creatorCaptionGestureSubject';
import type { EditOperationInput } from './editOperationInput';
import type { EntityPrecondition } from './entityPrecondition';

export interface CreatorCaptionGesturePreview {
  activityCursor: string;
  /** @maxItems 511 */
  alignmentEffects: CreatorCaptionAlignmentEffect[];
  baseProjectRevision: string;
  kind: CreatorCaptionGesturePreviewKind;
  /** @maxItems 512 */
  operations: EditOperationInput[];
  outputDigest: string;
  /** @maxItems 2048 */
  preconditions: EntityPrecondition[];
  subject: CreatorCaptionGestureSubject;
}
