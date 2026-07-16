import type { CreatorTimelineAlignmentEffectHandling } from './creatorTimelineAlignmentEffectHandling';

export interface CreatorTimelineAlignmentEffect {
  alignmentId: string;
  handling: CreatorTimelineAlignmentEffectHandling;
  /** @pattern ^[1-9][0-9]*$ */
  revision: string;
  /**
     * @minimum 0
     * @maximum 64
     */
  targetCount: number;
}
