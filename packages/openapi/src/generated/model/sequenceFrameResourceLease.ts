import type { RationalTime } from './rationalTime';
import type { SequenceFrameResourceLeaseMimeType } from './sequenceFrameResourceLeaseMimeType';

export interface SequenceFrameResourceLease {
  byteSize: string;
  expiresAt: string;
  frameIndex: string;
  mimeType: SequenceFrameResourceLeaseMimeType;
  readOnlyPath: string;
  requestedTime: RationalTime;
  resourceId: string;
  sequenceTime: RationalTime;
  sha256: string;
}
