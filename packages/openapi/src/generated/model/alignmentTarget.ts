import type { AlignmentTargetType } from './alignmentTargetType';
import type { CaptionAlignmentTarget } from './captionAlignmentTarget';
import type { ClipAlignmentTarget } from './clipAlignmentTarget';
import type { TimelineAlignmentTarget } from './timelineAlignmentTarget';

export interface AlignmentTarget {
  caption?: CaptionAlignmentTarget;
  clip?: ClipAlignmentTarget;
  timeline?: TimelineAlignmentTarget;
  type: AlignmentTargetType;
}
