import type { EditProposal } from './editProposal';

export interface EditProposalResult {
  activityCursor: string;
  proposal: EditProposal;
  replayed: boolean;
}
