import type { CreatorTimelineAlignmentEffect } from './creatorTimelineAlignmentEffect';
import type { CreatorTimelineClipEffect } from './creatorTimelineClipEffect';
import type { CreatorTimelineGesturePreviewKind } from './creatorTimelineGesturePreviewKind';
import type { CreatorTimelineGesturePreviewScope } from './creatorTimelineGesturePreviewScope';
import type { EditOperationInput } from './editOperationInput';
import type { EntityPrecondition } from './entityPrecondition';

export interface CreatorTimelineGesturePreview {
  activityCursor: string;
  /** @maxItems 64 */
  affectedClipIds: string[];
  /** @maxItems 2048 */
  alignmentEffects: CreatorTimelineAlignmentEffect[];
  baseProjectRevision: string;
  /**
     * @minItems 1
     * @maxItems 64
     */
  clipEffects: CreatorTimelineClipEffect[];
  /** @maxItems 128 */
  createdClipLocals: string[];
  kind: CreatorTimelineGesturePreviewKind;
  /** @maxItems 512 */
  operations: EditOperationInput[];
  outputDigest: string;
  /** @maxItems 2048 */
  preconditions: EntityPrecondition[];
  scope: CreatorTimelineGesturePreviewScope;
  seedClipId: string;
}
