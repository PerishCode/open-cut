import type { MediaLeaseRequestPurpose } from './mediaLeaseRequestPurpose';

export interface MediaLeaseRequest {
  /** @pattern ^[1-9][0-9]*$ */
  assetRevision: string;
  audioStreamId?: string;
  fingerprint: string;
  purpose: MediaLeaseRequestPurpose;
  videoStreamId?: string;
}
