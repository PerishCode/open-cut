import type { AlignmentTargetInputType } from './alignmentTargetInputType';
import type { EditReference } from './editReference';
import type { TimeRange } from './timeRange';

export interface AlignmentTargetInput {
  caption?: EditReference;
  clip?: EditReference;
  localRange?: TimeRange;
  /** @pattern ^[1-9][0-9]*$ */
  sequenceRevision?: string;
  timelineRange?: TimeRange;
  type: AlignmentTargetInputType;
}
