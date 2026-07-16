import type { EditProposal } from './editProposal';
import type { EditTransaction } from './editTransaction';

export interface EditCommitResult {
  activityCursor: string;
  proposal: EditProposal;
  replayed: boolean;
  transaction: EditTransaction;
}
