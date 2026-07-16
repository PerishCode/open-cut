import type { CreatorClipPlacementLane } from './creatorClipPlacementLane';
import type { EditOperationInput } from './editOperationInput';
import type { EntityPrecondition } from './entityPrecondition';
import type { TimeRange } from './timeRange';

export interface CreatorClipPlacementPreview {
  acceptedFingerprint: string;
  activityCursor: string;
  assetId: string;
  /** @pattern ^[1-9][0-9]*$ */
  assetRevision: string;
  baseProjectRevision: string;
  /**
     * @minItems 1
     * @maxItems 2
     */
  lanes: CreatorClipPlacementLane[];
  linked: boolean;
  /**
     * @minItems 1
     * @maxItems 2
     */
  operations: EditOperationInput[];
  outputDigest: string;
  /** @maxItems 4 */
  preconditions: EntityPrecondition[];
  sourceRange: TimeRange;
  timelineRange: TimeRange;
}
