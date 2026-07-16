import type { ActorRef } from './actorRef';
import type { EntityRevisionChange } from './entityRevisionChange';
import type { NormalizedEditOperation } from './normalizedEditOperation';

export interface EditTransaction {
  actor: ActorRef;
  /** @maxItems 2048 */
  changes: EntityRevisionChange[];
  committedAt: string;
  committedProjectRevision: string;
  digest: string;
  id: string;
  intent: string;
  /** @maxItems 512 */
  inverseOperations: NormalizedEditOperation[];
  /** @maxItems 512 */
  operations: NormalizedEditOperation[];
  projectId: string;
  proposalId: string;
  undoesTransactionId?: string;
}
