import type { CreatorCaptionAlignmentEffectHandling } from './creatorCaptionAlignmentEffectHandling';

export interface CreatorCaptionAlignmentEffect {
  alignmentId: string;
  handling: CreatorCaptionAlignmentEffectHandling;
  /** @pattern ^[1-9][0-9]*$ */
  revision: string;
  /**
     * @minimum 1
     * @maximum 64
     */
  targetCount: number;
}
