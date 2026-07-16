import type { MediaLeaseMimeType } from './mediaLeaseMimeType';
import type { MediaLeasePurpose } from './mediaLeasePurpose';
import type { MediaLeaseSchema } from './mediaLeaseSchema';
import type { RationalTime } from './rationalTime';
import type { SourcePreviewTrackTiming } from './sourcePreviewTrackTiming';

export interface MediaLease {
  artifactDigest: string;
  artifactId: string;
  assetId: string;
  /** @pattern ^[1-9][0-9]*$ */
  assetRevision: string;
  audio?: SourcePreviewTrackTiming;
  byteLength: string;
  etag: string;
  expiresAt: string;
  fingerprint: string;
  mimeType: MediaLeaseMimeType;
  projectId: string;
  purpose: MediaLeasePurpose;
  resourceId: string;
  sameOriginUrl: string;
  schema: MediaLeaseSchema;
  sourceEpoch: RationalTime;
  video?: SourcePreviewTrackTiming;
}
