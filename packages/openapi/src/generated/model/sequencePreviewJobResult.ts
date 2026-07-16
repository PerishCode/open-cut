import type { SequencePreviewJobResultKind } from './sequencePreviewJobResultKind';
import type { SequencePreviewJobResultState } from './sequencePreviewJobResultState';

export interface SequencePreviewJobResult {
  createdAt: string;
  id: string;
  kind: SequencePreviewJobResultKind;
  /**
     * @minimum 0
     * @maximum 10000
     */
  progressBasisPoints: number;
  renderPlanDigest?: string;
  resultArtifactId?: string;
  state: SequencePreviewJobResultState;
  /** @maxLength 256 */
  terminalErrorCode?: string;
  updatedAt: string;
}
