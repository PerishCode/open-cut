import type { MediaDiagnostic } from './mediaDiagnostic';
import type { SequencePreviewContinuation } from './sequencePreviewContinuation';
import type { SequencePreviewJobResult } from './sequencePreviewJobResult';
import type { SequencePreviewLease } from './sequencePreviewLease';
import type { SequencePreviewLeaseResultPurpose } from './sequencePreviewLeaseResultPurpose';
import type { SequencePreviewLeaseResultStage } from './sequencePreviewLeaseResultStage';
import type { SequencePreviewLeaseResultStatus } from './sequencePreviewLeaseResultStatus';

export interface SequencePreviewLeaseResult {
  continuation?: SequencePreviewContinuation;
  /** @maxItems 32 */
  diagnostics: MediaDiagnostic[];
  job?: SequencePreviewJobResult;
  lease?: SequencePreviewLease;
  projectId: string;
  purpose: SequencePreviewLeaseResultPurpose;
  sequenceId: string;
  sequenceRevision: string;
  stage?: SequencePreviewLeaseResultStage;
  status: SequencePreviewLeaseResultStatus;
}
