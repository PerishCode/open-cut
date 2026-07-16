import type { EditTransaction } from './editTransaction';

export interface TransactionHistoryPage {
  activityCursor: string;
  nextAfter?: string;
  /** @maxItems 100 */
  transactions: EditTransaction[];
}
