import type { CreatorTransactionHistoryItemActor } from './creatorTransactionHistoryItemActor';
import type { EntityRevisionChange } from './entityRevisionChange';

export interface CreatorTransactionHistoryItem {
  actor: CreatorTransactionHistoryItemActor;
  /** @maxItems 2048 */
  changes: EntityRevisionChange[];
  committedAt: string;
  committedProjectRevision: string;
  id: string;
  /** @maxLength 262144 */
  intent: string;
  undoesTransactionId?: string;
}
