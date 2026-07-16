import type { CommandReceipt } from './commandReceipt';

export interface TurnReceiptPage {
  nextAfter?: string;
  projectId: string;
  /** @maxItems 100 */
  receipts: CommandReceipt[];
  runId: string;
  turnId: string;
}
