import type { ActorRef } from './actorRef';
import type { EditImpact } from './editImpact';
import type { EditProposalStatus } from './editProposalStatus';
import type { EntityPrecondition } from './entityPrecondition';
import type { EntityRevisionChange } from './entityRevisionChange';
import type { LocalAllocation } from './localAllocation';
import type { NormalizedEditOperation } from './normalizedEditOperation';

export interface EditProposal {
  actor: ActorRef;
  /** @maxItems 1024 */
  allocation: LocalAllocation[];
  appliedTransactionId?: string;
  baseProjectRevision: string;
  /** @maxItems 2048 */
  changes: EntityRevisionChange[];
  createdAt: string;
  digest: string;
  id: string;
  impact: EditImpact;
  intent: string;
  /** @maxItems 512 */
  inversePreview: NormalizedEditOperation[];
  /** @maxItems 512 */
  operations: NormalizedEditOperation[];
  /** @maxItems 2048 */
  preconditions: EntityPrecondition[];
  projectId: string;
  requestId: string;
  runId?: string;
  sequenceId?: string;
  status: EditProposalStatus;
  turnId?: string;
}
