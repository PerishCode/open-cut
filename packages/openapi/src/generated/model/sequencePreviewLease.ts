import type { SequencePreviewLeaseMimeType } from './sequencePreviewLeaseMimeType';
import type { SequencePreviewLeasePurpose } from './sequencePreviewLeasePurpose';
import type { SequencePreviewLeaseSchema } from './sequencePreviewLeaseSchema';
import type { SequencePreviewMediaFacts } from './sequencePreviewMediaFacts';

export interface SequencePreviewLease {
  artifactDigest: string;
  artifactId: string;
  byteLength: string;
  etag: string;
  expiresAt: string;
  facts: SequencePreviewMediaFacts;
  mimeType: SequencePreviewLeaseMimeType;
  projectId: string;
  purpose: SequencePreviewLeasePurpose;
  renderPlanDigest: string;
  resourceId: string;
  sameOriginUrl: string;
  schema: SequencePreviewLeaseSchema;
  sequenceId: string;
  sequenceRevision: string;
}
