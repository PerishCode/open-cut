import type { AlignmentState } from './alignmentState';
import type { CaptionState } from './captionState';
import type { ClipState } from './clipState';
import type { LinkGroupState } from './linkGroupState';
import type { TimeRange } from './timeRange';

export interface SequenceWindowPage {
  activityCursor: string;
  /** @maxItems 2048 */
  alignments: AlignmentState[];
  /** @maxItems 512 */
  captions: CaptionState[];
  /** @maxItems 512 */
  clips: ClipState[];
  /** @maxItems 512 */
  linkGroups: LinkGroupState[];
  nextAfter?: string;
  range: TimeRange;
  sequenceId: string;
  sequenceRevision: string;
}
