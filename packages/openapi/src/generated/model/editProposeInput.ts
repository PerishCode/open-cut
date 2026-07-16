import type { EditOperationInput } from './editOperationInput';
import type { EntityPrecondition } from './entityPrecondition';

export interface EditProposeInput {
  /** @pattern ^[1-9][0-9]*$ */
  baseProjectRevision: string;
  /**
     * @minLength 1
     * @maxLength 4000
     */
  intent: string;
  /**
     * @minItems 1
     * @maxItems 512
     */
  operations: EditOperationInput[];
  /** @maxItems 2048 */
  preconditions: EntityPrecondition[];
  /** @pattern ^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$ */
  requestId: string;
}
