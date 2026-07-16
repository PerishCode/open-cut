import type { EditOperationInput } from './editOperationInput';
import type { EntityPrecondition } from './entityPrecondition';

export interface RoughCutDerivationPreview {
  activityCursor: string;
  baseProjectRevision: string;
  operation: EditOperationInput;
  outputDigest: string;
  /** @maxItems 2048 */
  preconditions: EntityPrecondition[];
}
