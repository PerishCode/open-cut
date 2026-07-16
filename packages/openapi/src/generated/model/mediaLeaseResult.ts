import type { MediaDiagnostic } from './mediaDiagnostic';
import type { MediaJobSummary } from './mediaJobSummary';
import type { MediaLease } from './mediaLease';
import type { MediaLeaseResultPurpose } from './mediaLeaseResultPurpose';
import type { MediaLeaseResultStage } from './mediaLeaseResultStage';
import type { MediaLeaseResultStatus } from './mediaLeaseResultStatus';

export interface MediaLeaseResult {
  assetId: string;
  /** @pattern ^[1-9][0-9]*$ */
  assetRevision: string;
  audioStreamId?: string;
  /** @maxItems 32 */
  diagnostics: MediaDiagnostic[];
  fingerprint: string;
  job: MediaJobSummary;
  lease?: MediaLease;
  projectId: string;
  purpose: MediaLeaseResultPurpose;
  stage?: MediaLeaseResultStage;
  status: MediaLeaseResultStatus;
  videoStreamId?: string;
}
