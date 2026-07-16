import type { CreatorTransactionHistoryItem } from './creatorTransactionHistoryItem';

export interface CreatorTransactionHistoryPage {
  activityCursor: string;
  nextBefore?: string;
  /** @maxItems 50 */
  transactions: CreatorTransactionHistoryItem[];
}
