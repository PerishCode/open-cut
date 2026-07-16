import type { SequenceFrameCoordinate } from './sequenceFrameCoordinate';
import type { SequenceFrameJobData } from './sequenceFrameJobData';
import type { SequenceFrameResourceLease } from './sequenceFrameResourceLease';
import type { SequenceFramesDataProfile } from './sequenceFramesDataProfile';
import type { SequenceFramesDataRecovery } from './sequenceFramesDataRecovery';
import type { SequenceFramesDataStatus } from './sequenceFramesDataStatus';

export interface SequenceFramesData {
  activityCursor: string;
  job: SequenceFrameJobData;
  profile: SequenceFramesDataProfile;
  projectId: string;
  recovery: SequenceFramesDataRecovery;
  /** @maxItems 8 */
  resources: SequenceFrameResourceLease[];
  /**
     * @minItems 1
     * @maxItems 8
     */
  samples: SequenceFrameCoordinate[];
  sequenceId: string;
  sequenceRevision: string;
  status: SequenceFramesDataStatus;
}
