import type { EditOperationInput } from './editOperationInput';
import type { EntityPrecondition } from './entityPrecondition';

export interface CaptionDerivationPreview {
  activityCursor: string;
  baseProjectRevision: string;
  /** @maxLength 64 */
  language: string;
  operation: EditOperationInput;
  /** @maxItems 260 */
  preconditions: EntityPrecondition[];
}
