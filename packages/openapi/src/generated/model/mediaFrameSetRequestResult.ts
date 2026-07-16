import type { FrameResourceLease } from './frameResourceLease';
import type { MediaFrameSetRequestResultStatus } from './mediaFrameSetRequestResultStatus';
import type { MediaJobSummary } from './mediaJobSummary';

export interface MediaFrameSetRequestResult {
  activityCursor: string;
  artifactId?: string;
  job: MediaJobSummary;
  /** @maxItems 8 */
  resources: FrameResourceLease[];
  status: MediaFrameSetRequestResultStatus;
}
