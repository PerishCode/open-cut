import type { FrameResourceLeaseMimeType } from './frameResourceLeaseMimeType';
import type { RationalTime } from './rationalTime';

export interface FrameResourceLease {
  byteSize: string;
  expiresAt: string;
  mimeType: FrameResourceLeaseMimeType;
  readOnlyPath: string;
  requestedTime: RationalTime;
  resourceId: string;
  sha256: string;
  sourceTime: RationalTime;
}
