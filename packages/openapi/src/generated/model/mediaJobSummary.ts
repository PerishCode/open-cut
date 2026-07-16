import type { MediaJobPrerequisite } from './mediaJobPrerequisite';
import type { MediaJobSummaryKind } from './mediaJobSummaryKind';
import type { MediaJobSummaryState } from './mediaJobSummaryState';

export interface MediaJobSummary {
  createdAt: string;
  id: string;
  kind: MediaJobSummaryKind;
  /** @maxItems 8 */
  prerequisites: MediaJobPrerequisite[];
  /**
     * @minimum 0
     * @maximum 10000
     */
  progressBasisPoints: number;
  resultArtifactId?: string;
  state: MediaJobSummaryState;
  /**
     * @minLength 1
     * @maxLength 256
     */
  terminalErrorCode?: string;
  updatedAt: string;
}
