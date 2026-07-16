import type { SequencePreviewContinuation } from './sequencePreviewContinuation';
import type { SequencePreviewLeaseRequestOperation } from './sequencePreviewLeaseRequestOperation';
import type { SequencePreviewLeaseRequestPurpose } from './sequencePreviewLeaseRequestPurpose';

export interface SequencePreviewLeaseRequest {
  continuation?: SequencePreviewContinuation;
  expectedSequenceRevision: string;
  operation: SequencePreviewLeaseRequestOperation;
  purpose: SequencePreviewLeaseRequestPurpose;
}
