import type { AlignmentStateStatus } from './alignmentStateStatus';
import type { AlignmentTarget } from './alignmentTarget';

export interface AlignmentState {
  id: string;
  narrativeNodeId: string;
  narrativeNodeRevision: string;
  revision: string;
  sequenceId: string;
  status: AlignmentStateStatus;
  /**
     * @minItems 1
     * @maxItems 64
     */
  targets: AlignmentTarget[];
}
